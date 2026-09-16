-- Reverse of 015_active_security.up.sql: drop EDR/SIEM integration objects.
DROP TABLE IF EXISTS siem_forward_queue;
DROP TABLE IF EXISTS siem_forwarders;
DROP TABLE IF EXISTS security_events;
DROP TABLE IF EXISTS edr_agent_mapping;
DROP TABLE IF EXISTS edr_integrations;
