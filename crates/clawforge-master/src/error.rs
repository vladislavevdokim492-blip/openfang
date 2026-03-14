use thiserror::Error;

/// Master 错误类型
#[derive(Error, Debug)]
pub enum MasterError {
    #[error("worker not found: {0}")]
    WorkerNotFound(String),

    #[error("no available workers")]
    NoAvailableWorkers,

    #[error("task not found: {0}")]
    TaskNotFound(String),

    #[error("unit task not found: {0}")]
    UnitNotFound(String),

    #[error("worker pool full")]
    PoolFull,

    #[error("assignment failed: {0}")]
    AssignmentFailed(String),

    #[error("split failed: {0}")]
    SplitFailed(String),

    #[error("retry exhausted for unit {0}: max {1} retries")]
    RetryExhausted(String, u32),

    #[error("timeout: {0}")]
    Timeout(String),

    #[error("internal error: {0}")]
    Internal(String),
}
