-- Copyright (c) 2026 hangtiancheng
--
-- Permission is hereby granted, free of charge, to any person obtaining a copy
-- of this software and associated documentation files (the "Software"), to deal
-- in the Software without restriction, including without limitation the rights
-- to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
-- copies of the Software, and to permit persons to whom the Software is
-- furnished to do so, subject to the following conditions:
--
-- The above copyright notice and this permission notice shall be included in
-- all copies or substantial portions of the Software.
--
-- THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
-- IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
-- FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
-- AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
-- LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
-- OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
-- SOFTWARE.

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
