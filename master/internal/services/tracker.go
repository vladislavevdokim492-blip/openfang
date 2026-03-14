package services

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/clawforge/p3-go/claw/master/internal/models"
)

// MasterTrackerService Master 级进度跟踪+结果汇总 (ORCH-010 + ORCH-011)
type MasterTrackerService struct {
	mu          sync.RWMutex
	units       map[string]*models.Unit          // unitID -> Unit
	subtaskUnits map[string][]string              // subtaskID -> []unitID
	subscribers map[string][]chan *UnitProgressEvent // subtaskID -> subscribers
	results     map[string]*SubtaskAggregation    // subtaskID -> aggregation
}

// UnitProgressEvent 单元进度事件
type UnitProgressEvent struct {
	SubtaskID string    `json:"subtask_id"`
	UnitID    string    `json:"unit_id"`
	WorkerID  string    `json:"worker_id"`
	Status    string    `json:"status"`
	Progress  int       `json:"progress"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// SubtaskAggregation 子任务汇总
type SubtaskAggregation struct {
	SubtaskID      string                   `json:"subtask_id"`
	TotalUnits     int                      `json:"total_units"`
	CompletedUnits int                      `json:"completed_units"`
	FailedUnits    int                      `json:"failed_units"`
	RunningUnits   int                      `json:"running_units"`
	OverallProgress int                     `json:"overall_progress"`
	Status         string                   `json:"status"` // running, completed, failed, partial
	MergedOutput   map[string]interface{}   `json:"merged_output"`
	UnitResults    []UnitResult             `json:"unit_results"`
	StartedAt      *time.Time               `json:"started_at"`
	CompletedAt    *time.Time               `json:"completed_at"`
}

// UnitResult 单元结果
type UnitResult struct {
	UnitID       string                 `json:"unit_id"`
	WorkerID     string                 `json:"worker_id"`
	Status       string                 `json:"status"`
	Output       map[string]interface{} `json:"output,omitempty"`
	ErrorMessage string                 `json:"error_message,omitempty"`
	Duration     time.Duration          `json:"duration"`
}

// NewMasterTrackerService 创建 Master 跟踪服务
func NewMasterTrackerService() *MasterTrackerService {
	return &MasterTrackerService{
		units:        make(map[string]*models.Unit),
		subtaskUnits: make(map[string][]string),
		subscribers:  make(map[string][]chan *UnitProgressEvent),
		results:      make(map[string]*SubtaskAggregation),
	}
}

// TrackUnits 注册需要跟踪的单元任务
func (s *MasterTrackerService) TrackUnits(subtaskID string, units []*models.Unit) {
	s.mu.Lock()
	defer s.mu.Unlock()

	unitIDs := make([]string, 0, len(units))
	for _, u := range units {
		s.units[u.ID] = u
		unitIDs = append(unitIDs, u.ID)
	}
	s.subtaskUnits[subtaskID] = unitIDs

	now := time.Now()
	s.results[subtaskID] = &SubtaskAggregation{
		SubtaskID:  subtaskID,
		TotalUnits: len(units),
		Status:     "running",
		StartedAt:  &now,
	}

	log.Printf("[MasterTracker] Tracking %d units for subtask %s", len(units), subtaskID)
}

// UpdateUnitProgress 更新单元进度 (ORCH-010)
func (s *MasterTrackerService) UpdateUnitProgress(unitID string, progress int, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	unit, exists := s.units[unitID]
	if !exists {
		return fmt.Errorf("unit not found: %s", unitID)
	}

	unit.Progress = progress
	unit.UpdatedAt = time.Now()

	// 重新计算子任务总进度
	s.recalculateProgress(unit.SubtaskID)

	// 通知订阅者
	s.notify(unit.SubtaskID, &UnitProgressEvent{
		SubtaskID: unit.SubtaskID,
		UnitID:    unitID,
		WorkerID:  unit.WorkerID,
		Status:    string(unit.Status),
		Progress:  progress,
		Message:   message,
		Timestamp: time.Now(),
	})

	return nil
}

// UpdateUnitStatus 更新单元状态
func (s *MasterTrackerService) UpdateUnitStatus(unitID string, status models.UnitStatus, output map[string]interface{}, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	unit, exists := s.units[unitID]
	if !exists {
		return fmt.Errorf("unit not found: %s", unitID)
	}

	unit.Status = status
	unit.UpdatedAt = time.Now()

	if output != nil {
		unit.Output = output
	}
	if errMsg != "" {
		unit.ErrorMessage = errMsg
	}

	now := time.Now()
	switch status {
	case models.UnitStatusRunning:
		unit.StartedAt = &now
	case models.UnitStatusCompleted, models.UnitStatusFailed, models.UnitStatusCancelled:
		unit.CompletedAt = &now
		if status == models.UnitStatusCompleted {
			unit.Progress = 100
		}
	}

	// 重新计算并检查子任务完成状态
	s.recalculateProgress(unit.SubtaskID)
	s.checkSubtaskCompletion(unit.SubtaskID)

	// 通知
	s.notify(unit.SubtaskID, &UnitProgressEvent{
		SubtaskID: unit.SubtaskID,
		UnitID:    unitID,
		WorkerID:  unit.WorkerID,
		Status:    string(status),
		Progress:  unit.Progress,
		Message:   errMsg,
		Timestamp: now,
	})

	return nil
}

// GetSubtaskAggregation 获取子任务汇总 (ORCH-011)
func (s *MasterTrackerService) GetSubtaskAggregation(subtaskID string) (*SubtaskAggregation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agg, exists := s.results[subtaskID]
	if !exists {
		return nil, fmt.Errorf("subtask not found: %s", subtaskID)
	}

	// 构建完整结果
	result := *agg
	result.UnitResults = make([]UnitResult, 0)

	unitIDs := s.subtaskUnits[subtaskID]
	for _, uid := range unitIDs {
		unit := s.units[uid]
		if unit == nil {
			continue
		}

		ur := UnitResult{
			UnitID:       unit.ID,
			WorkerID:     unit.WorkerID,
			Status:       string(unit.Status),
			Output:       unit.Output,
			ErrorMessage: unit.ErrorMessage,
		}
		if unit.StartedAt != nil && unit.CompletedAt != nil {
			ur.Duration = unit.CompletedAt.Sub(*unit.StartedAt)
		}
		result.UnitResults = append(result.UnitResults, ur)
	}

	// 合并输出
	result.MergedOutput = s.mergeOutputs(subtaskID)

	return &result, nil
}

// Subscribe 订阅子任务进度
func (s *MasterTrackerService) Subscribe(subtaskID string) chan *UnitProgressEvent {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan *UnitProgressEvent, 100)
	s.subscribers[subtaskID] = append(s.subscribers[subtaskID], ch)
	return ch
}

// Unsubscribe 取消订阅
func (s *MasterTrackerService) Unsubscribe(subtaskID string, ch chan *UnitProgressEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	subs := s.subscribers[subtaskID]
	for i, sub := range subs {
		if sub == ch {
			s.subscribers[subtaskID] = append(subs[:i], subs[i+1:]...)
			close(ch)
			break
		}
	}
}

// IsSubtaskComplete 检查子任务是否完成
func (s *MasterTrackerService) IsSubtaskComplete(subtaskID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agg, exists := s.results[subtaskID]
	if !exists {
		return false
	}
	return agg.Status == "completed" || agg.Status == "failed" || agg.Status == "partial"
}

// recalculateProgress 重新计算子任务进度
func (s *MasterTrackerService) recalculateProgress(subtaskID string) {
	unitIDs := s.subtaskUnits[subtaskID]
	if len(unitIDs) == 0 {
		return
	}

	totalProgress := 0
	completed := 0
	failed := 0
	running := 0

	for _, uid := range unitIDs {
		unit := s.units[uid]
		if unit == nil {
			continue
		}
		totalProgress += unit.Progress
		switch unit.Status {
		case models.UnitStatusCompleted:
			completed++
		case models.UnitStatusFailed:
			failed++
		case models.UnitStatusRunning:
			running++
		}
	}

	agg := s.results[subtaskID]
	if agg != nil {
		agg.OverallProgress = totalProgress / len(unitIDs)
		agg.CompletedUnits = completed
		agg.FailedUnits = failed
		agg.RunningUnits = running
	}
}

// checkSubtaskCompletion 检查子任务是否全部完成
func (s *MasterTrackerService) checkSubtaskCompletion(subtaskID string) {
	agg := s.results[subtaskID]
	if agg == nil {
		return
	}

	allDone := (agg.CompletedUnits + agg.FailedUnits) == agg.TotalUnits
	if !allDone {
		return
	}

	now := time.Now()
	agg.CompletedAt = &now

	if agg.FailedUnits == 0 {
		agg.Status = "completed"
		log.Printf("[MasterTracker] Subtask %s completed: %d/%d units", subtaskID, agg.CompletedUnits, agg.TotalUnits)
	} else if agg.CompletedUnits == 0 {
		agg.Status = "failed"
		log.Printf("[MasterTracker] Subtask %s failed: all %d units failed", subtaskID, agg.TotalUnits)
	} else {
		agg.Status = "partial"
		log.Printf("[MasterTracker] Subtask %s partial: %d completed, %d failed", subtaskID, agg.CompletedUnits, agg.FailedUnits)
	}
}

// mergeOutputs 合并单元输出
func (s *MasterTrackerService) mergeOutputs(subtaskID string) map[string]interface{} {
	merged := make(map[string]interface{})
	outputs := make([]interface{}, 0)

	unitIDs := s.subtaskUnits[subtaskID]
	for _, uid := range unitIDs {
		unit := s.units[uid]
		if unit != nil && unit.Status == models.UnitStatusCompleted && unit.Output != nil {
			outputs = append(outputs, unit.Output)
		}
	}

	merged["unit_outputs"] = outputs
	outputJSON, _ := json.Marshal(merged)
	_ = outputJSON

	return merged
}

// notify 通知订阅者
func (s *MasterTrackerService) notify(subtaskID string, event *UnitProgressEvent) {
	subs := s.subscribers[subtaskID]
	for _, ch := range subs {
		select {
		case ch <- event:
		default:
			log.Printf("[MasterTracker] Subscriber channel full for subtask %s", subtaskID)
		}
	}
}
