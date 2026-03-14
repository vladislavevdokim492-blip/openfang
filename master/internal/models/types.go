package models

import "time"

// UnitStatus 单元任务状态
type UnitStatus string

const (
	UnitStatusPending   UnitStatus = "pending"
	UnitStatusAssigned  UnitStatus = "assigned"
	UnitStatusRunning   UnitStatus = "running"
	UnitStatusCompleted UnitStatus = "completed"
	UnitStatusFailed    UnitStatus = "failed"
	UnitStatusCancelled UnitStatus = "cancelled"
)

// Unit 单元任务（微观任务拆分后的最小执行单元）
type Unit struct {
	ID           string                 `json:"id"`
	SubtaskID    string                 `json:"subtask_id"`
	Description  string                 `json:"description"`
	Input        map[string]interface{} `json:"input"`
	Output       map[string]interface{} `json:"output,omitempty"`
	Status       UnitStatus             `json:"status"`
	Progress     int                    `json:"progress"`
	WorkerID     string                 `json:"worker_id,omitempty"`
	Priority     int                    `json:"priority"`
	Order        int                    `json:"order"`
	DependsOn    []string               `json:"depends_on,omitempty"`
	RetryCount   int                    `json:"retry_count"`
	MaxRetries   int                    `json:"max_retries"`
	ErrorMessage string                 `json:"error_message,omitempty"`
	AssignedAt   *time.Time             `json:"assigned_at,omitempty"`
	StartedAt    *time.Time             `json:"started_at,omitempty"`
	CompletedAt  *time.Time             `json:"completed_at,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

// WorkerNode Worker 节点
type WorkerNode struct {
	ID            string    `json:"id"`
	Address       string    `json:"address"`
	Port          int       `json:"port"`
	Status        string    `json:"status"` // idle, busy, offline
	CurrentUnitID string    `json:"current_unit_id,omitempty"`
	CPUUsage      float64   `json:"cpu_usage"`
	MemoryUsage   float64   `json:"memory_usage"`
	DiskUsage     float64   `json:"disk_usage"`
	MaxConcurrent int       `json:"max_concurrent"`
	ActiveUnits   int       `json:"active_units"`
	TotalExecuted int64     `json:"total_executed"`
	TotalFailed   int64     `json:"total_failed"`
	Tags          []string  `json:"tags"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	RegisteredAt  time.Time `json:"registered_at"`
}

// SubtaskContext 子任务上下文（从 Orchestrator 接收）
type SubtaskContext struct {
	SubtaskID      string                 `json:"subtask_id"`
	TaskID         string                 `json:"task_id"`
	OrgID          string                 `json:"org_id"`
	Description    string                 `json:"description"`
	Input          map[string]interface{} `json:"input"`
	Priority       int                    `json:"priority"`
	MaxRetries     int                    `json:"max_retries"`
	TimeoutSeconds int                    `json:"timeout_seconds"`
}
