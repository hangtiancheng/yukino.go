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
