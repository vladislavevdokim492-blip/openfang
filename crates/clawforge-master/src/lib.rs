//! # ClawForge Master
//!
//! Claw Master 负责微观任务拆分、Worker 池管理、任务分配和进度跟踪。
//! 接收 Orchestrator 下发的子任务，进一步拆分为单元任务分配给 Worker 执行。

pub mod error;
pub mod models;
pub mod pool;
pub mod scheduler;
pub mod splitter;
pub mod tracker;
pub mod retry;

pub use error::MasterError;
pub use pool::WorkerPool;
pub use scheduler::TaskScheduler;
pub use splitter::MicroTaskSplitter;
pub use tracker::ProgressTracker;
pub use retry::RetryManager;
