package services

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/clawforge/p3-go/claw/master/internal/models"
	"github.com/google/uuid"
)

// WorkerService Claw Worker 服务 (ORCH-015/016/017/018)
type WorkerService struct {
	mu            sync.RWMutex
	workerID      string
	status        string // idle, busy, offline
	currentUnit   *models.Unit
	config        WorkerConfig
	progressCh    chan *ProgressUpdate
	resultCh      chan *ResultUpdate
	errorCh       chan *ErrorUpdate
	cancelFuncs   map[string]context.CancelFunc // unitID -> cancel
	executionFunc ExecutionFunc                  // 实际执行逻辑
}

// WorkerConfig Worker 配置
type WorkerConfig struct {
	WorkerID        string
	MaxConcurrent   int
	ProgressInterval time.Duration // 进度上报间隔
	HeartbeatInterval time.Duration
	MasterAddress   string
	MasterPort      int
}

// ExecutionFunc 任务执行函数类型
type ExecutionFunc func(ctx context.Context, unit *models.Unit, progressFn func(int, string)) (map[string]interface{}, error)

// ProgressUpdate 进度更新
type ProgressUpdate struct {
	UnitID    string    `json:"unit_id"`
	SubtaskID string    `json:"subtask_id"`
	Progress  int       `json:"progress"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// ResultUpdate 结果更新
type ResultUpdate struct {
	UnitID    string                 `json:"unit_id"`
	SubtaskID string                 `json:"subtask_id"`
	Status    string                 `json:"status"`
	Output    map[string]interface{} `json:"output"`
	Timestamp time.Time              `json:"timestamp"`
}

// ErrorUpdate 错误更新
type ErrorUpdate struct {
	UnitID       string    `json:"unit_id"`
	SubtaskID    string    `json:"subtask_id"`
	ErrorCode    string    `json:"error_code"`
	ErrorMessage string    `json:"error_message"`
	StackTrace   string    `json:"stack_trace"`
	Recoverable  bool      `json:"recoverable"`
	RetryCount   int       `json:"retry_count"`
	Timestamp    time.Time `json:"timestamp"`
}

// NewWorkerService 创建 Worker 服务
func NewWorkerService(cfg WorkerConfig, execFn ExecutionFunc) *WorkerService {
	if cfg.WorkerID == "" {
		cfg.WorkerID = fmt.Sprintf("worker_%s", uuid.New().String()[:8])
	}
	if cfg.MaxConcurrent == 0 {
		cfg.MaxConcurrent = 1
	}
	if cfg.ProgressInterval == 0 {
		cfg.ProgressInterval = 5 * time.Second
	}
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = 10 * time.Second
	}

	w := &WorkerService{
		workerID:    cfg.WorkerID,
		status:      "idle",
		config:      cfg,
		progressCh:  make(chan *ProgressUpdate, 100),
		resultCh:    make(chan *ResultUpdate, 100),
		errorCh:     make(chan *ErrorUpdate, 100),
		cancelFuncs: make(map[string]context.CancelFunc),
		executionFunc: execFn,
	}

	// 启动上报协程
	go w.progressReporter()
	go w.resultReporter()
	go w.errorReporter()
	go w.heartbeatLoop()

	return w
}

// ExecuteUnit 接收并执行单元任务 (ORCH-015)
func (w *WorkerService) ExecuteUnit(ctx context.Context, unit *models.Unit) error {
	w.mu.Lock()
	if w.status == "offline" {
		w.mu.Unlock()
		return fmt.Errorf("worker is offline")
	}

	// 创建可取消的上下文
	execCtx, cancel := context.WithCancel(ctx)
	w.cancelFuncs[unit.ID] = cancel
	w.currentUnit = unit
	w.status = "busy"
	w.mu.Unlock()

	log.Printf("[Worker %s] Executing unit %s: %s", w.workerID, unit.ID, unit.Description)

	// 更新单元状态
	now := time.Now()
	unit.Status = models.UnitStatusRunning
	unit.StartedAt = &now
	unit.WorkerID = w.workerID

	// 上报开始
	w.reportProgress(unit, 0, "execution started")

	// 执行任务
	go func() {
		defer func() {
			if r := recover(); r != nil {
				w.reportError(unit, "PANIC", fmt.Sprintf("panic: %v", r), true)
			}
			w.mu.Lock()
			delete(w.cancelFuncs, unit.ID)
			w.currentUnit = nil
			w.status = "idle"
			w.mu.Unlock()
		}()

		// 进度回调
		progressFn := func(progress int, message string) {
			w.reportProgress(unit, progress, message)
		}

		// 执行
		output, err := w.executionFunc(execCtx, unit, progressFn)
		if err != nil {
			// 错误上报 (ORCH-018)
			recoverable := isRecoverable(err)
			w.reportError(unit, "EXEC_ERROR", err.Error(), recoverable)

			// 结果上报 - 失败
			w.resultCh <- &ResultUpdate{
				UnitID:    unit.ID,
				SubtaskID: unit.SubtaskID,
				Status:    string(models.UnitStatusFailed),
				Timestamp: time.Now(),
			}
			return
		}

		// 结果返回 (ORCH-017)
		w.reportProgress(unit, 100, "execution completed")
		w.resultCh <- &ResultUpdate{
			UnitID:    unit.ID,
			SubtaskID: unit.SubtaskID,
			Status:    string(models.UnitStatusCompleted),
			Output:    output,
			Timestamp: time.Now(),
		}

		log.Printf("[Worker %s] Unit %s completed successfully", w.workerID, unit.ID)
	}()

	return nil
}

// CancelExecution 取消执行
func (w *WorkerService) CancelExecution(unitID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	cancel, exists := w.cancelFuncs[unitID]
	if !exists {
		return fmt.Errorf("unit %s not running on this worker", unitID)
	}

	cancel()
	delete(w.cancelFuncs, unitID)
	log.Printf("[Worker %s] Unit %s cancelled", w.workerID, unitID)
	return nil
}

// GetStatus 获取 Worker 状态
func (w *WorkerService) GetStatus() *WorkerStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	status := &WorkerStatus{
		WorkerID:      w.workerID,
		Status:        w.status,
		CPUUsage:      0, // 需要系统调用获取
		MemoryUsage:   float64(memStats.Alloc) / float64(memStats.Sys) * 100,
		DiskUsage:     0,
		LastHeartbeat: time.Now(),
	}

	if w.currentUnit != nil {
		status.CurrentUnitID = w.currentUnit.ID
	}

	return status
}

// WorkerStatus Worker 状态
type WorkerStatus struct {
	WorkerID      string    `json:"worker_id"`
	Status        string    `json:"status"`
	CurrentUnitID string    `json:"current_unit_id,omitempty"`
	CPUUsage      float64   `json:"cpu_usage"`
	MemoryUsage   float64   `json:"memory_usage"`
	DiskUsage     float64   `json:"disk_usage"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

// ProgressChan 获取进度通道（供上层消费）
func (w *WorkerService) ProgressChan() <-chan *ProgressUpdate {
	return w.progressCh
}

// ResultChan 获取结果通道
func (w *WorkerService) ResultChan() <-chan *ResultUpdate {
	return w.resultCh
}

// ErrorChan 获取错误通道
func (w *WorkerService) ErrorChan() <-chan *ErrorUpdate {
	return w.errorCh
}

// reportProgress 上报进度 (ORCH-016)
func (w *WorkerService) reportProgress(unit *models.Unit, progress int, message string) {
	update := &ProgressUpdate{
		UnitID:    unit.ID,
		SubtaskID: unit.SubtaskID,
		Progress:  progress,
		Message:   message,
		Timestamp: time.Now(),
	}

	select {
	case w.progressCh <- update:
	default:
		log.Printf("[Worker %s] Progress channel full, dropping update for unit %s", w.workerID, unit.ID)
	}
}

// reportError 上报错误 (ORCH-018)
func (w *WorkerService) reportError(unit *models.Unit, code, message string, recoverable bool) {
	update := &ErrorUpdate{
		UnitID:       unit.ID,
		SubtaskID:    unit.SubtaskID,
		ErrorCode:    code,
		ErrorMessage: message,
		Recoverable:  recoverable,
		RetryCount:   unit.RetryCount,
		Timestamp:    time.Now(),
	}

	select {
	case w.errorCh <- update:
	default:
		log.Printf("[Worker %s] Error channel full, dropping error for unit %s", w.workerID, unit.ID)
	}
}

// progressReporter 进度上报协程
func (w *WorkerService) progressReporter() {
	for update := range w.progressCh {
		// 实际实现中通过 gRPC 上报给 Master
		log.Printf("[Worker %s] Progress: unit=%s progress=%d%% msg=%s",
			w.workerID, update.UnitID, update.Progress, update.Message)
	}
}

// resultReporter 结果上报协程
func (w *WorkerService) resultReporter() {
	for update := range w.resultCh {
		log.Printf("[Worker %s] Result: unit=%s status=%s",
			w.workerID, update.UnitID, update.Status)
	}
}

// errorReporter 错误上报协程
func (w *WorkerService) errorReporter() {
	for update := range w.errorCh {
		log.Printf("[Worker %s] Error: unit=%s code=%s msg=%s recoverable=%v",
			w.workerID, update.UnitID, update.ErrorCode, update.ErrorMessage, update.Recoverable)
	}
}

// heartbeatLoop 心跳循环
func (w *WorkerService) heartbeatLoop() {
	ticker := time.NewTicker(w.config.HeartbeatInterval)
	defer ticker.Stop()

	for range ticker.C {
		status := w.GetStatus()
		// 实际实现中通过 gRPC 发送心跳给 Master
		_ = status
	}
}

// Shutdown 优雅关闭
func (w *WorkerService) Shutdown() {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 取消所有正在执行的任务
	for unitID, cancel := range w.cancelFuncs {
		cancel()
		log.Printf("[Worker %s] Cancelled unit %s during shutdown", w.workerID, unitID)
	}

	w.status = "offline"
	close(w.progressCh)
	close(w.resultCh)
	close(w.errorCh)

	log.Printf("[Worker %s] Shutdown complete", w.workerID)
}

// isRecoverable 判断错误是否可恢复
func isRecoverable(err error) bool {
	msg := err.Error()
	nonRecoverable := []string{
		"invalid input",
		"permission denied",
		"authentication failed",
		"cancelled",
	}
	for _, nr := range nonRecoverable {
		if msg == nr {
			return false
		}
	}
	return true
}
