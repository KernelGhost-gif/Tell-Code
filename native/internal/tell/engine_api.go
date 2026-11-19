package tell

// ValidExecutionEngine reports whether engine names a supported execution policy.
func ValidExecutionEngine(engine string) bool { return validExecutionEngine(engine) }
