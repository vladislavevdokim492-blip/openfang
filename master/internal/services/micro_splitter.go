package services

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/clawforge/p3-go/claw/master/internal/models"
	"github.com/google/uuid"
)

// MicroSplitterService 微观任务拆分服务 (ORCH-007)
// 将子任务拆分为单元级执行任务
type MicroSplitterService struct {
	config MicroSplitterConfig
}

// MicroSplitterConfig 微观拆分配置
type MicroSplitterConfig struct {
	MaxUnitsPerSubtask int           // 每个子任务最大单元数
	SplitTimeout       time.Duration // 拆分超时
}

// MicroSplitResult 微观拆分结果
type MicroSplitResult struct {
	SubtaskID string         `json:"subtask_id"`
	Units     []*models.Unit `json:"units"`
	Strategy  string         `json:"strategy"`
}

// NewMicroSplitterService 创建微观拆分服务
func NewMicroSplitterService(cfg MicroSplitterConfig) *MicroSplitterService {
	if cfg.MaxUnitsPerSubtask == 0 {
		cfg.MaxUnitsPerSubtask = 10
	}
	if cfg.SplitTimeout == 0 {
		cfg.SplitTimeout = 30 * time.Second
	}
	return &MicroSplitterService{config: cfg}
}

// SplitSubtask 将子任务拆分为单元任务
func (s *MicroSplitterService) SplitSubtask(ctx context.Context, subtask *models.SubtaskContext) (*MicroSplitResult, error) {
	ctx, cancel := context.WithTimeout(ctx, s.config.SplitTimeout)
	defer cancel()

	log.Printf("[MicroSplitter] Splitting subtask %s: %s", subtask.SubtaskID, subtask.Description)

	// 尝试 AI 拆分
	units, err := s.aiSplit(ctx, subtask)
	if err != nil {
		log.Printf("[MicroSplitter] AI split failed: %v, using simple split", err)
		units = s.simpleSplit(subtask)
	}

	// 限制单元数
	if len(units) > s.config.MaxUnitsPerSubtask {
		units = units[:s.config.MaxUnitsPerSubtask]
	}

	// 设置顺序
	for i, u := range units {
		u.Order = i
		u.SubtaskID = subtask.SubtaskID
		u.MaxRetries = subtask.MaxRetries
		if u.MaxRetries == 0 {
			u.MaxRetries = 3
		}
	}

	strategy := "sequential"
	if len(units) > 1 && !s.hasDependencies(units) {
		strategy = "parallel"
	}

	log.Printf("[MicroSplitter] Subtask %s split into %d units (strategy=%s)", subtask.SubtaskID, len(units), strategy)

	return &MicroSplitResult{
		SubtaskID: subtask.SubtaskID,
		Units:     units,
		Strategy:  strategy,
	}, nil
}

// aiSplit AI 拆分（当前降级为简单拆分）
func (s *MicroSplitterService) aiSplit(ctx context.Context, subtask *models.SubtaskContext) ([]*models.Unit, error) {
	// TODO: 集成 AI 模型进行智能拆分
	return nil, fmt.Errorf("AI model not configured")
}

// simpleSplit 简单拆分
func (s *MicroSplitterService) simpleSplit(subtask *models.SubtaskContext) []*models.Unit {
	now := time.Now()
	return []*models.Unit{
		{
			ID:          uuid.New().String(),
			SubtaskID:   subtask.SubtaskID,
			Description: subtask.Description,
			Input:       subtask.Input,
			Status:      models.UnitStatusPending,
			Priority:    subtask.Priority,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}
}

// hasDependencies 检查是否有依赖关系
func (s *MicroSplitterService) hasDependencies(units []*models.Unit) bool {
	for _, u := range units {
		if len(u.DependsOn) > 0 {
			return true
		}
	}
	return false
}
