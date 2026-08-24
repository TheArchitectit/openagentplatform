package telemetry

// ---- Convenience recorders ----------------------------------------------
//
// These thin wrappers protect callers from a nil collector (which happens
// when InitMeter has not been called yet) and keep call sites short.

// RecordAPIRequest increments api_requests_total.
func RecordAPIRequest(method, path, status string) {
	if APIRequestsTotal != nil {
		APIRequestsTotal.WithLabelValues(method, path, status).Inc()
	}
}

// RecordNATSMessage increments nats_messages_total.
func RecordNATSMessage(subject, direction string) {
	if NATSMessagesTotal != nil {
		NATSMessagesTotal.WithLabelValues(subject, direction).Inc()
	}
}

// RecordAgentHeartbeat increments agent_heartbeats_total.
func RecordAgentHeartbeat(agentID, status string) {
	if AgentHeartbeatsTotal != nil {
		AgentHeartbeatsTotal.WithLabelValues(agentID, status).Inc()
	}
}

// RecordCheckResult increments check_results_total.
func RecordCheckResult(checkType, status string) {
	if CheckResultsTotal != nil {
		CheckResultsTotal.WithLabelValues(checkType, status).Inc()
	}
}

// RecordAlertTransition increments alert_transitions_total.
func RecordAlertTransition(fromState, toState string) {
	if AlertTransitionsTotal != nil {
		AlertTransitionsTotal.WithLabelValues(fromState, toState).Inc()
	}
}

// RecordA2ATask increments a2a_tasks_total.
func RecordA2ATask(adapter, status string) {
	if A2ATasksTotal != nil {
		A2ATasksTotal.WithLabelValues(adapter, status).Inc()
	}
}

// RecordDBQuery increments db_queries_total.
func RecordDBQuery(operation string) {
	if DBQueriesTotal != nil {
		DBQueriesTotal.WithLabelValues(operation).Inc()
	}
}

// RecordBytesByAdapter adds bytes to bytes_by_adapter_total.
func RecordBytesByAdapter(adapter, direction string, n int64) {
	if BytesByAdapterTotal != nil {
		BytesByAdapterTotal.WithLabelValues(adapter, direction).Add(float64(n))
	}
}

// ObserveHTTPRequestDuration records a duration sample for
// http_request_duration_seconds.
func ObserveHTTPRequestDuration(method, path string, seconds float64) {
	if HTTPRequestDurationSeconds != nil {
		HTTPRequestDurationSeconds.WithLabelValues(method, path).Observe(seconds)
	}
}

// ObserveDBQueryDuration records a duration sample for
// db_query_duration_seconds.
func ObserveDBQueryDuration(operation string, seconds float64) {
	if DBQueryDurationSeconds != nil {
		DBQueryDurationSeconds.WithLabelValues(operation).Observe(seconds)
	}
}

// ObserveCheckExecutionDuration records a duration sample for
// check_execution_duration_seconds.
func ObserveCheckExecutionDuration(checkType string, seconds float64) {
	if CheckExecutionDurationSeconds != nil {
		CheckExecutionDurationSeconds.WithLabelValues(checkType).Observe(seconds)
	}
}

// ObserveAdapterInvokeDuration records a duration sample for
// adapter_invoke_duration_seconds.
func ObserveAdapterInvokeDuration(adapter string, seconds float64) {
	if AdapterInvokeDurationSeconds != nil {
		AdapterInvokeDurationSeconds.WithLabelValues(adapter).Observe(seconds)
	}
}

// SetAgentsOnline writes the current agents_online gauge value.
func SetAgentsOnline(agentID string, n int64) {
	if AgentsOnline != nil {
		AgentsOnline.WithLabelValues(agentID).Set(float64(n))
	}
}

// SetActiveAlerts writes the current active_alerts gauge value.
func SetActiveAlerts(severity string, n int64) {
	if ActiveAlerts != nil {
		ActiveAlerts.WithLabelValues(severity).Set(float64(n))
	}
}

// SetActiveShellSessions writes the current active_shell_sessions gauge value.
func SetActiveShellSessions(n int64) {
	if ActiveShellSessions != nil {
		ActiveShellSessions.Set(float64(n))
	}
}

// SetAdapterPoolProcs writes the current adapter_pool_processes gauge value.
func SetAdapterPoolProcs(state string, n int64) {
	if AdapterPoolProcs != nil {
		AdapterPoolProcs.WithLabelValues(state).Set(float64(n))
	}
}

// SetCostTotalByAdapter writes the current cost_total_by_adapter gauge value.
func SetCostTotalByAdapter(adapter string, usd float64) {
	if CostTotalByAdapter != nil {
		CostTotalByAdapter.WithLabelValues(adapter).Set(usd)
	}
}
