package store

type migration struct {
	version int
	file    string
}

// Versions are persisted identities, independent of slice positions and SQL
// filename prefixes. Existing databases recorded recommendation as 18,
// announcement as 19, and execution settings as 20. Append new identities;
// never renumber the published migrations or sort this registry by filename.
var migrationRegistry = []migration{
	{1, "migrations/001_init.sql"},
	{2, "migrations/002_evidence_order.sql"},
	{3, "migrations/003_strip_provider_thoughts.sql"},
	{4, "migrations/004_model_reporting.sql"},
	{5, "migrations/005_model_catalog.sql"},
	{6, "migrations/006_site_metrics.sql"},
	{7, "migrations/007_article_exports.sql"},
	{8, "migrations/008_external_references.sql"},
	{9, "migrations/009_provider_policies.sql"},
	{10, "migrations/010_reference_snapshots.sql"},
	{11, "migrations/011_network_settings.sql"},
	{12, "migrations/012_diagnostics.sql"},
	{13, "migrations/013_storage_management.sql"},
	{14, "migrations/014_model_route_priorities.sql"},
	{15, "migrations/015_request_history_index.sql"},
	{16, "migrations/016_example_showcases.sql"},
	{17, "migrations/017_request_shares.sql"},
	{18, "migrations/019_model_recommendation.sql"},
	{19, "migrations/020_site_announcement.sql"},
	{20, "migrations/018_execution_settings.sql"},
	{21, "migrations/021_example_process.sql"},
	{22, "migrations/022_announcement_history.sql"},
	{23, "migrations/023_storage_disk_stop_percent.sql"},
	{24, "migrations/024_configurable_storage_admission.sql"},
	{25, "migrations/025_storage_stop_line_99_percent.sql"},
	{26, "migrations/026_simple_disk_admission.sql"},
	{27, "migrations/027_raise_video_input_limits.sql"},
}
