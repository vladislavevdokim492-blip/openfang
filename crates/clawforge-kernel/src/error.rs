//! Kernel-specific error types.

use clawforge_types::error::ClawForgeError;
use thiserror::Error;

/// Kernel error type wrapping ClawForgeError with kernel-specific context.
#[derive(Error, Debug)]
pub enum KernelError {
    /// A wrapped ClawForgeError.
    #[error(transparent)]
    ClawForge(#[from] ClawForgeError),

    /// The kernel failed to boot.
    #[error("Boot failed: {0}")]
    BootFailed(String),
}

/// Alias for kernel results.
pub type KernelResult<T> = Result<T, KernelError>;
