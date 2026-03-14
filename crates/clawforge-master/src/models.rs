use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;

/// Worker 状态
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub enum WorkerStatus {
    Idle,
    Busy,
    Offline,
    Draining, // 正在排空，不接受新任务
}

/// Worker 信息
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WorkerInfo {
    pub worker_id: String,
    pub address: String,
    pub port: u16,
    pub status: WorkerStatus,
    pub current_unit_id: Option<String>,
    pub cpu_usage: f64,
    pub memory_usage: f64,
    pub disk_usage: f64,
    pub max_concurrent: u32,
    pub active_units: u32,
    pub last_heartbeat: DateTime<Utc>,
    pub registered_at: DateTime<Utc>,
    pub metadata: HashMap<String, String>,
}

/// 单元任务状态
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub enum UnitStatus {
    Pending,
    Assigned,
    Running,
    Completed,
    Failed,
    Cancelled,
    Retrying,
}

/// 单元任务
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct UnitTask {
    pub unit_id: String,
    pub subtask_id: String,
    pub description: String,
    pub input: serde_json::Value,
    pub status: UnitStatus,
    pub progress: u32,
    pub worker_id: Option<String>,
    pub output: Option<serde_json::Value>,
    pub error_message: Option<String>,
    pub retry_count: u32,
    pub max_retries: u32,
    pub timeout_seconds: u32,
    pub depends_on: Vec<String>,
    pub assigned_at: Option<DateTime<Utc>>,
    pub started_at: Option<DateTime<Utc>>,
    pub completed_at: Option<DateTime<Utc>>,
    pub created_at: DateTime<Utc>,
}

/// 子任务上下文（从 Orchestrator 接收）
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SubtaskContext {
    pub subtask_id: String,
    pub task_id: String,
    pub org_id: String,
    pub description: String,
    pub input: serde_json::Value,
    pub priority: TaskPriority,
    pub max_retries: u32,
    pub timeout_seconds: u32,
}

/// 任务优先级
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq, PartialOrd, Ord)]
pub enum TaskPriority {
    Low = 1,
    Normal = 2,
    High = 3,
    Critical = 4,
}

impl Default for TaskPriority {
    fn default() -> Self {
        TaskPriority::Normal
    }
}

/// 进度报告
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProgressReport {
    pub unit_id: String,
    pub subtask_id: String,
    pub worker_id: String,
    pub progress: u32,
    pub message: String,
    pub timestamp: DateTime<Utc>,
}

/// 结果报告
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ResultReport {
    pub unit_id: String,
    pub subtask_id: String,
    pub worker_id: String,
    pub status: UnitStatus,
    pub output: Option<serde_json::Value>,
    pub quality_score: Option<f64>,
    pub completed_at: DateTime<Utc>,
}

/// 错误报告
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ErrorReport {
    pub unit_id: String,
    pub subtask_id: String,
    pub worker_id: String,
    pub error_code: String,
    pub error_message: String,
    pub stack_trace: Option<String>,
    pub recoverable: bool,
    pub retry_count: u32,
    pub timestamp: DateTime<Utc>,
}

/// 汇总结果
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AggregatedResult {
    pub subtask_id: String,
    pub total_units: usize,
    pub completed: usize,
    pub failed: usize,
    pub merged_output: serde_json::Value,
    pub quality_score: f64,
    pub duration_ms: u64,
}
