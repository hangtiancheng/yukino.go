
CREATE TABLE IF NOT EXISTS `task`
(
    `id`         bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT 'Primary key ID',
    `app`        varchar(255) NOT NULL COMMENT 'App name',
    `timer_id`   bigint(20) NOT NULL COMMENT 'Timer ID',
    `output`     varchar(256) DEFAULT NULL COMMENT 'Execution result',
    `run_timer`  datetime     NOT NULL COMMENT 'Execution time',
    `cost_time`  int(8) DEFAULT NULL COMMENT 'Execution cost in milliseconds',
    `status`     int(4) NOT NULL COMMENT 'Current status',
    `created_at` datetime     NOT NULL COMMENT 'Creation time',
    `updated_at` datetime     NOT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT 'Update time',
    `deleted_at` datetime     DEFAULT NULL COMMENT 'Deletion time',
    PRIMARY KEY (`id`) USING BTREE COMMENT 'Primary key index',
    UNIQUE KEY `idx_def_timer` (`timer_id`,`run_timer`) USING BTREE COMMENT 'Timer execution time index',
    KEY `idx_run_timer` (`run_timer`) COMMENT 'Execution time index'
) ENGINE=InnoDB AUTO_INCREMENT=1 DEFAULT CHARSET=utf8mb4;
