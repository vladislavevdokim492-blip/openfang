package services

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/clawforge/p3-go/claw/master/internal/models"
)

// RetryService 错误处理与重试服务 (ORCH-013)
type RetryService struct {
	mu          sync.Mutex
	workerPool  *WorkerPoolService
	tracker     *MasterTrackerService
	config      RetryConfig
	retryQueue  chan *RetryItem
	stopCh      chan struct{}
}

// RetryConfig 重试配置
type RetryConfig struct {
	MaxRetries      int           // 最大重试次数（默认 3）
	BaseDelay       time.Duration // 基础延迟（默认 1s）
	MaxDelay        time.Duration // 最大延迟（默认 30s）
	BackoffFactor   float64       // 退避因子（默认 2.0）
	QueueSize       int           // 重试队列大小
	WorkerCount     int           // 重试处理协程数
}

// RetryItem 重试项
type RetryItem struct {
	Unit       *models.Unit
	RetryCount int
	LastError  string
	NextRetry  time.Time
	CreatedAt  time.Time
}

// NewRetryService 创建重试服务
func NewRetryService(pool *WorkerPoolService, tracker *MasterTrackerService, cfg RetryConfig) *RetryService {
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.BaseDelay == 0 {
		cfg.BaseDelay = 1 * time.Second
	}
	if cfg.MaxDelay == 0 {
		cfg.MaxDelay = 30 * time.Second
	}
	if cfg.BackoffFactor == 0 {
		cfg.BackoffFactor = 2.0
	}
	if cfg.QueueSize == 0 {
		cfg.QueueSize = 1000
	}
	if cfg.WorkerCount == 0 {
		cfg.WorkerCount = 3
	}

	s := &RetryService{
		workerPool: pool,
		tracker:    tracker,
		config:     cfg,
		retryQueue: make(chan *RetryItem, cfg.QueueSize),
		stopCh:     make(chan struct{}),
	}

	// 启动重试处理协程
	for i := 0; i < cfg.WorkerCount; i++ {
		go s.retryWorker(i)
	}

	return s
}

// HandleUnitFailure 处理单元任务失败
func (s *RetryService) HandleUnitFailure(unit *models.Unit, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	unit.RetryCount++
	unit.ErrorMessage = errMsg

	maxRetries := unit.MaxRetries
	if maxRetries == 0 {
		maxRetries = s.config.MaxRetries
	}

	// 检查是否超过最大重试次数
	if unit.RetryCount > maxRetries {
		log.Printf("[Retry] Unit %s exceeded max retries (%d/%d), marking as failed",
			unit.ID, unit.RetryCount, maxRetries)

		s.tracker.UpdateUnitStatus(unit.ID, models.UnitStatusFailed, nil, errMsg)
		return fmt.Errorf("max retries exceeded for unit %s", unit.ID)
	}

	// 计算下次重试时间（指数退避）
	delay := s.calculateDelay(unit.RetryCount)
	nextRetry := time.Now().Add(delay)

	log.Printf("[Retry] Unit %s scheduled for retry %d/%d in %v (error: %s)",
		unit.ID, unit.RetryCount, maxRetries, delay, errMsg)

	// 重置单元状态
	unit.Status = models.UnitStatusPending
	unit.WorkerID = ""
	unit.AssignedAt = nil
	unit.StartedAt = nil
	unit.CompletedAt = nil

	// 加入重试队列
	item := &RetryItem{
		Unit:       unit,
		RetryCount: unit.RetryCount,
		LastError:  errMsg,
		NextRetry:  nextRetry,
		CreatedAt:  time.Now(),
	}

	select {
	case s.retryQueue <- item:
		return nil
	default:
		return fmt.Errorf("retry queue is full")
	}
}

// calculateDelay 计算指数退避延迟
func (s *RetryService) calculateDelay(retryCount int) time.Duration {
	// delay = baseDelay * backoffFactor^(retryCount-1)
	delay := float64(s.config.BaseDelay) * math.Pow(s.config.BackoffFactor, float64(retryCount-1))

	if time.Duration(delay) > s.config.MaxDelay {
		return s.config.MaxDelay
	}

	return time.Duration(delay)
}

// retryWorker 重试处理协程
func (s *RetryService) retryWorker(id int) {
	for {
		select {
		case item := <-s.retryQueue:
			s.processRetry(id, item)
		case <-s.stopCh:
			return
		}
	}
}

// processRetry 处理重试
func (s *RetryService) processRetry(workerID int, item *RetryItem) {
	// 等待到下次重试时间
	waitDuration := time.Until(item.NextRetry)
	if waitDuration > 0 {
		time.Sleep(waitDuration)
	}

	log.Printf("[Retry] Worker %d processing retry for unit %s (attempt %d)",
		workerID, item.Unit.ID, item.RetryCount)

	// 重新分配到 Worker
	worker, err := s.workerPool.AssignUnit(item.Unit)
	if err != nil {
		log.Printf("[Retry] Failed to reassign unit %s: %v", item.Unit.ID, err)
		// 如果分配失败，再次加入重试队列
		item.NextRetry = time.Now().Add(5 * time.Second)
		select {
		case s.retryQueue <- item:
		default:
			log.Printf("[Retry] Retry queue full, unit %s dropped", item.Unit.ID)
			s.tracker.UpdateUnitStatus(item.Unit.ID, models.UnitStatusFailed, nil, "retry queue full")
		}
		return
	}

	log.Printf("[Retry] Unit %s reassigned to worker %s", item.Unit.ID, worker.ID)
}

// Stop 停止重试服务
func (s *RetryService) Stop() {
	close(s.stopCh)
}

// GetRetryStats 获取重试统计
func (s *RetryService) GetRetryStats() *RetryStats {
	return &RetryStats{
		QueueLength: len(s.retryQueue),
		QueueCap:    cap(s.retryQueue),
		MaxRetries:  s.config.MaxRetries,
		BaseDelay:   s.config.BaseDelay.String(),
		MaxDelay:    s.config.MaxDelay.String(),
	}
}

// RetryStats 重试统计
type RetryStats struct {
	QueueLength int    `json:"queue_length"`
	QueueCap    int    `json:"queue_capacity"`
	MaxRetries  int    `json:"max_retries"`
	BaseDelay   string `json:"base_delay"`
	MaxDelay    string `json:"max_delay"`
}

// ShouldRetry 判断错误是否应该重试
func ShouldRetry(errMsg string) bool {
	// 不可重试的错误
	nonRetryable := []string{
		"invalid input",
		"permission denied",
		"authentication failed",
		"not found",
		"cancelled",
	}

	for _, nr := range nonRetryable {
		if errMsg == nr {
			return false
		}
	}

	return true
}

// RetryWithContext 带上下文的重试执行
func RetryWithContext(ctx context.Context, maxRetries int, baseDelay time.Duration, fn func() error) error {
	var lastErr error
	for i := 0; i <= maxRetries; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		if !ShouldRetry(lastErr.Error()) {
			return lastErr
		}

		if i < maxRetries {
			delay := baseDelay * time.Duration(math.Pow(2, float64(i)))
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return fmt.Errorf("max retries exceeded: %w", lastErr)
}
