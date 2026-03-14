use crate::error::MasterError;
use crate::models::{SubtaskContext, UnitStatus, UnitTask};
use chrono::Utc;
use tracing::info;
use uuid::Uuid;

/// 微观任务拆分器 (ORCH-007)
///
/// 将 Orchestrator 下发的子任务进一步拆分为单元级子任务
pub struct MicroTaskSplitter {
    max_units_per_subtask: usize,
}

impl MicroTaskSplitter {
    pub fn new(max_units_per_subtask: usize) -> Self {
        Self {
            max_units_per_subtask: if max_units_per_subtask == 0 {
                50
            } else {
                max_units_per_subtask
            },
        }
    }

    /// 拆分子任务为单元任务
    pub async fn split(&self, ctx: &SubtaskContext) -> Result<Vec<UnitTask>, MasterError> {
        info!(
            subtask_id = %ctx.subtask_id,
            "Splitting subtask into unit tasks"
        );

        // 尝试 AI 驱动拆分
        let units = match self.ai_split(ctx).await {
            Ok(units) => units,
            Err(e) => {
                tracing::warn!("AI split failed: {}, using simple split", e);
                self.simple_split(ctx)
            }
        };

        // 限制单元任务数量
        let units = if units.len() > self.max_units_per_subtask {
            units[..self.max_units_per_subtask].to_vec()
        } else {
            units
        };

        info!(
            subtask_id = %ctx.subtask_id,
            unit_count = units.len(),
            "Subtask split complete"
        );

        Ok(units)
    }

    /// AI 驱动拆分（预留接口）
    async fn ai_split(&self, ctx: &SubtaskContext) -> Result<Vec<UnitTask>, MasterError> {
        // TODO: 调用 LLM API 进行智能拆分
        // 当前降级为简单拆分
        Err(MasterError::SplitFailed("AI not configured".into()))
    }

    /// 简单拆分策略（降级方案）
    fn simple_split(&self, ctx: &SubtaskContext) -> Vec<UnitTask> {
        let now = Utc::now();
        vec![UnitTask {
            unit_id: format!("unit_{}", Uuid::new_v4()),
            subtask_id: ctx.subtask_id.clone(),
            description: ctx.description.clone(),
            input: ctx.input.clone(),
            status: UnitStatus::Pending,
            progress: 0,
            worker_id: None,
            output: None,
            error_message: None,
            retry_count: 0,
            max_retries: ctx.max_retries,
            timeout_seconds: ctx.timeout_seconds,
            depends_on: vec![],
            assigned_at: None,
            started_at: None,
            completed_at: None,
            created_at: now,
        }]
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::models::TaskPriority;

    #[tokio::test]
    async fn test_simple_split() {
        let splitter = MicroTaskSplitter::new(50);
        let ctx = SubtaskContext {
            subtask_id: "st_1".into(),
            task_id: "task_1".into(),
            org_id: "org_1".into(),
            description: "Test subtask".into(),
            input: serde_json::json!({"key": "value"}),
            priority: TaskPriority::Normal,
            max_retries: 3,
            timeout_seconds: 300,
        };

        let units = splitter.split(&ctx).await.unwrap();
        assert!(!units.is_empty());
        assert_eq!(units[0].subtask_id, "st_1");
        assert_eq!(units[0].status, UnitStatus::Pending);
    }
}
