-- Reverse of 014_eve_monitoring.up.sql: drop hypervisor monitoring objects.
DROP TABLE IF EXISTS hypervisor_events;
DROP TABLE IF EXISTS hypervisor_resources;
DROP TABLE IF EXISTS hypervisor_clusters;
