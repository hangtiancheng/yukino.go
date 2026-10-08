
CREATE TABLE IF NOT EXISTS `tx_record`
(
    `id`                       bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT 'Primary key ID',
    `status`                   varchar(16) NOT NULL COMMENT 'Transaction status: hanging/successful/failure',
    `component_try_statuses`   json DEFAULT NULL COMMENT 'Try status of each component: hanging/successful/failure',
    `deleted_at`        datetime     DEFAULT NULL COMMENT 'Deletion time',
    `created_at`        datetime     NOT NULL COMMENT 'Creation time',
    `updated_at`        datetime     DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT 'Update time',
    PRIMARY KEY (`id`) USING BTREE COMMENT 'Primary key index',
    KEY `idx_status` (`status`) COMMENT 'Transaction status index'
) ENGINE=InnoDB AUTO_INCREMENT=1 DEFAULT CHARSET=utf8mb4 COMMENT 'Transaction log record';
