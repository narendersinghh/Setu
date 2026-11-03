<?php
return [
// Parallel AI engine shares the queue table with the Claude batch mode
"ALTER TABLE ai_batch ADD mode VARCHAR(10) NOT NULL DEFAULT 'batch',ADD attempts TINYINT UNSIGNED NOT NULL DEFAULT 0,ADD next_at DATETIME NULL,ADD started_at DATETIME NULL,ADD step_id INT UNSIGNED NOT NULL DEFAULT 0,ADD claim VARCHAR(20) NULL,ADD KEY mq (mode,status)",
// Bulk reports now use the parallel engine with any AI provider; the Claude batch stays as the optional cheapest mode
"UPDATE settings SET v='parallel' WHERE k='ai_batch' AND v IN ('auto','on')",
];
