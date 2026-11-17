
CREATE TABLE IF NOT EXISTS `timer`
(
    `id`                bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT 'Primary key ID',
    `app`               varchar(255) NOT NULL COMMENT 'App name',
    `name`              varchar(255) NOT NULL COMMENT 'Timer name',
    `status`            smallint(255) NOT NULL COMMENT 'Timer status: 1=disabled, 2=enabled',
    `cron`              varchar(255) NOT NULL COMMENT 'Cron expression',
    `notify_http_param` json         DEFAULT NULL COMMENT 'HTTP callback parameters',
    `deleted_at`        datetime     DEFAULT NULL COMMENT 'Deletion time',
    `created_at`        datetime     NOT NULL COMMENT 'Creation time',
    `updated_at`        datetime     DEFAULT NULL ON UPDATE CURRENT_TIMESTAMP COMMENT 'Update time',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uni_app` (`app`,`name`) USING BTREE COMMENT 'App name index'
) ENGINE=InnoDB AUTO_INCREMENT=1 DEFAULT CHARSET=utf8;
