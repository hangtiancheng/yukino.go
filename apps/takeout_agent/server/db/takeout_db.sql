-- ============================================================
-- takeout_db schema and seed data (English edition)
-- Requires MySQL 8 (JSON columns and JSON_OVERLAPS).
-- Import: mysql -u <user> -p < db/takeout_db.sql
-- ============================================================

CREATE DATABASE IF NOT EXISTS `takeout_db` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE `takeout_db`;

SET NAMES utf8mb4;
SET FOREIGN_KEY_CHECKS = 0;

-- ----------------------------
-- Table structure for takeout_messages
-- ----------------------------
DROP TABLE IF EXISTS `takeout_messages`;
CREATE TABLE `takeout_messages` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `session_id` varchar(64) NOT NULL,
  `role` varchar(32) NOT NULL,
  `content` text NOT NULL,
  `intent` varchar(64) NULL DEFAULT NULL,
  `agent_trace_id` varchar(128) NULL DEFAULT NULL,
  `created_at` datetime NOT NULL,
  PRIMARY KEY (`id`),
  INDEX `idx_message_session` (`session_id`, `created_at`)
) ENGINE = InnoDB CHARACTER SET = utf8mb4 COLLATE = utf8mb4_unicode_ci ROW_FORMAT = DYNAMIC;

-- ----------------------------
-- Table structure for takeout_request_trace
-- ----------------------------
DROP TABLE IF EXISTS `takeout_request_trace`;
CREATE TABLE `takeout_request_trace` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `trace_id` varchar(128) NOT NULL,
  `session_id` varchar(64) NOT NULL,
  `user_id` bigint NOT NULL,
  `status` varchar(32) NOT NULL,
  `event_count` int NOT NULL DEFAULT 0,
  `duration_ms` bigint NULL DEFAULT NULL,
  `error_message` text NULL,
  `trace_json` json NOT NULL,
  `created_at` datetime NOT NULL,
  `updated_at` datetime NOT NULL,
  `expected_intent` varchar(64) NULL DEFAULT NULL,
  `expected_slots` json NULL,
  `expected_clarify_action` varchar(32) NULL DEFAULT NULL,
  `labeled_by` bigint NULL DEFAULT NULL,
  `labeled_at` datetime NULL DEFAULT NULL,
  `label_note` varchar(512) NULL DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE INDEX `uk_request_trace` (`trace_id`),
  INDEX `idx_request_trace_session` (`session_id`, `created_at`),
  INDEX `idx_request_trace_user` (`user_id`, `created_at`),
  INDEX `idx_request_trace_status` (`status`, `created_at`),
  INDEX `idx_request_trace_label` (`expected_intent`, `labeled_at`)
) ENGINE = InnoDB CHARACTER SET = utf8mb4 COLLATE = utf8mb4_unicode_ci ROW_FORMAT = DYNAMIC;

-- ----------------------------
-- Table structure for takeout_sessions
-- ----------------------------
DROP TABLE IF EXISTS `takeout_sessions`;
CREATE TABLE `takeout_sessions` (
  `id` varchar(64) NOT NULL,
  `user_id` bigint NOT NULL,
  `phase` varchar(64) NOT NULL,
  `slots` json NOT NULL,
  `last_recommendations` json NOT NULL,
  `created_at` datetime NOT NULL,
  `updated_at` datetime NOT NULL,
  PRIMARY KEY (`id`),
  INDEX `idx_session_user` (`user_id`, `updated_at`)
) ENGINE = InnoDB CHARACTER SET = utf8mb4 COLLATE = utf8mb4_unicode_ci ROW_FORMAT = DYNAMIC;

-- ----------------------------
-- Table structure for takeout_slot_option
-- ----------------------------
DROP TABLE IF EXISTS `takeout_slot_option`;
CREATE TABLE `takeout_slot_option` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `slot_name` varchar(64) NOT NULL,
  `option_value` varchar(64) NOT NULL,
  `sort_order` int NOT NULL DEFAULT 0,
  `enabled` tinyint NOT NULL DEFAULT 1,
  `created_at` datetime NOT NULL,
  `updated_at` datetime NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE INDEX `uk_slot_option` (`slot_name`, `option_value`),
  INDEX `idx_slot_enabled` (`slot_name`, `enabled`, `sort_order`)
) ENGINE = InnoDB CHARACTER SET = utf8mb4 COLLATE = utf8mb4_unicode_ci ROW_FORMAT = DYNAMIC;

-- ----------------------------
-- Records of takeout_slot_option
-- ----------------------------
INSERT INTO `takeout_slot_option` (`slot_name`, `option_value`, `sort_order`, `enabled`, `created_at`, `updated_at`) VALUES
('mealTime', 'Breakfast', 10, 1, NOW(), NOW()),
('mealTime', 'Brunch', 20, 1, NOW(), NOW()),
('mealTime', 'Lunch', 30, 1, NOW(), NOW()),
('mealTime', 'Afternoon Tea', 40, 1, NOW(), NOW()),
('mealTime', 'Dinner', 50, 1, NOW(), NOW()),
('mealTime', 'Late Night Snack', 60, 1, NOW(), NOW()),
('mealTime', 'Extra Meal', 70, 1, NOW(), NOW()),
('mealTime', 'Three Meals', 80, 1, NOW(), NOW()),
('mood', 'Tired', 10, 1, NOW(), NOW()),
('mood', 'Irritable', 20, 1, NOW(), NOW()),
('mood', 'Happy', 30, 1, NOW(), NOW()),
('mood', 'Anxious', 40, 1, NOW(), NOW()),
('mood', 'Down', 50, 1, NOW(), NOW()),
('mood', 'Calm', 60, 1, NOW(), NOW()),
('mood', 'Stressed', 70, 1, NOW(), NOW()),
('mood', 'No Appetite', 80, 1, NOW(), NOW()),
('mood', 'Want To Relax', 90, 1, NOW(), NOW()),
('mood', 'Treat Myself', 100, 1, NOW(), NOW()),
('scene', 'Work', 10, 1, NOW(), NOW()),
('scene', 'Campus', 20, 1, NOW(), NOW()),
('scene', 'Home', 30, 1, NOW(), NOW()),
('scene', 'Weekend', 40, 1, NOW(), NOW()),
('scene', 'Overtime', 50, 1, NOW(), NOW()),
('scene', 'Post-Workout', 60, 1, NOW(), NOW()),
('scene', 'Commute', 70, 1, NOW(), NOW()),
('scene', 'Gathering', 80, 1, NOW(), NOW()),
('scene', 'Solo', 90, 1, NOW(), NOW()),
('scene', 'Travel', 100, 1, NOW(), NOW()),
('scene', 'Late Night', 110, 1, NOW(), NOW()),
('healthGoal', 'Fat Loss', 10, 1, NOW(), NOW()),
('healthGoal', 'Light', 20, 1, NOW(), NOW()),
('healthGoal', 'Stomach-Friendly', 30, 1, NOW(), NOW()),
('healthGoal', 'High Protein', 40, 1, NOW(), NOW()),
('healthGoal', 'Balanced', 50, 1, NOW(), NOW()),
('healthGoal', 'Cooling', 60, 1, NOW(), NOW()),
('healthGoal', 'Low Oil', 70, 1, NOW(), NOW()),
('healthGoal', 'Low Salt', 80, 1, NOW(), NOW()),
('healthGoal', 'Low Sugar', 90, 1, NOW(), NOW()),
('healthGoal', 'Energy Boost', 100, 1, NOW(), NOW()),
('healthGoal', 'Muscle Gain', 110, 1, NOW(), NOW()),
('healthGoal', 'Low Carb', 120, 1, NOW(), NOW()),
('healthGoal', 'Easy Digest', 130, 1, NOW(), NOW()),
('healthGoal', 'Warming', 140, 1, NOW(), NOW()),
('cuisine', 'Sichuan', 10, 1, NOW(), NOW()),
('cuisine', 'Cantonese', 20, 1, NOW(), NOW()),
('cuisine', 'Hunan', 30, 1, NOW(), NOW()),
('cuisine', 'Jiangzhe', 40, 1, NOW(), NOW()),
('cuisine', 'Northeastern', 50, 1, NOW(), NOW()),
('cuisine', 'Shandong', 60, 1, NOW(), NOW()),
('cuisine', 'Fujian', 70, 1, NOW(), NOW()),
('cuisine', 'Yunnan', 80, 1, NOW(), NOW()),
('cuisine', 'Xinjiang', 90, 1, NOW(), NOW()),
('cuisine', 'Light Meal', 100, 1, NOW(), NOW()),
('cuisine', 'Western', 110, 1, NOW(), NOW()),
('cuisine', 'Japanese', 120, 1, NOW(), NOW()),
('cuisine', 'Korean', 130, 1, NOW(), NOW()),
('cuisine', 'Southeast Asian', 140, 1, NOW(), NOW()),
('cuisine', 'Hot Pot', 150, 1, NOW(), NOW()),
('cuisine', 'BBQ', 160, 1, NOW(), NOW()),
('cuisine', 'Seafood', 170, 1, NOW(), NOW()),
('cuisine', 'Vegetarian', 180, 1, NOW(), NOW()),
('cuisine', 'Home Style', 190, 1, NOW(), NOW()),
('cuisine', 'Street Food', 200, 1, NOW(), NOW()),
('cuisine', 'Noodles', 210, 1, NOW(), NOW()),
('cuisine', 'Porridge & Soup', 220, 1, NOW(), NOW()),
('cuisine', 'Fast Food', 230, 1, NOW(), NOW()),
('cuisine', 'Dessert', 240, 1, NOW(), NOW()),
('taste', 'Light', 10, 1, NOW(), NOW()),
('taste', 'Spicy', 20, 1, NOW(), NOW()),
('taste', 'Mildly Spicy', 30, 1, NOW(), NOW()),
('taste', 'Medium Spicy', 40, 1, NOW(), NOW()),
('taste', 'Numbing Spicy', 50, 1, NOW(), NOW()),
('taste', 'Sweet', 60, 1, NOW(), NOW()),
('taste', 'Sweet & Sour', 70, 1, NOW(), NOW()),
('taste', 'Savory', 80, 1, NOW(), NOW()),
('taste', 'Umami', 90, 1, NOW(), NOW()),
('taste', 'Sauce Flavor', 100, 1, NOW(), NOW()),
('taste', 'Garlic', 110, 1, NOW(), NOW()),
('taste', 'Tomato', 120, 1, NOW(), NOW()),
('taste', 'Curry', 130, 1, NOW(), NOW()),
('taste', 'Milky', 140, 1, NOW(), NOW()),
('taste', 'Rich', 150, 1, NOW(), NOW()),
('taste', 'Smoky', 160, 1, NOW(), NOW()),
('convenience', 'Quick', 10, 1, NOW(), NOW()),
('convenience', 'Slow Enjoy', 20, 1, NOW(), NOW()),
('convenience', 'Easy Takeout', 30, 1, NOW(), NOW()),
('convenience', 'Dine-In', 40, 1, NOW(), NOW()),
('convenience', 'No Queue', 50, 1, NOW(), NOW()),
('convenience', 'Minimal Utensils', 60, 1, NOW(), NOW()),
('convenience', 'Solo Meal', 70, 1, NOW(), NOW()),
('convenience', 'Sharing', 80, 1, NOW(), NOW()),
('convenience', 'Meal Prep', 90, 1, NOW(), NOW()),
('convenience', 'On The Go', 100, 1, NOW(), NOW());

-- ----------------------------
-- Table structure for meal_item
-- ----------------------------
DROP TABLE IF EXISTS `meal_item`;
CREATE TABLE `meal_item` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `source_type` varchar(16) NOT NULL,
  `owner_user_id` bigint NULL DEFAULT NULL,
  `name` varchar(128) NOT NULL,
  `meal_time` json NOT NULL,
  `mood` json NOT NULL,
  `scene` json NOT NULL,
  `health_goal` json NOT NULL,
  `cuisine` json NOT NULL,
  `taste` json NOT NULL,
  `convenience` json NOT NULL,
  `created_at` datetime NOT NULL,
  `updated_at` datetime NOT NULL,
  PRIMARY KEY (`id`),
  INDEX `idx_public_meal_source` (`source_type`),
  INDEX `idx_private_meal_source` (`owner_user_id`, `source_type`)
) ENGINE = InnoDB CHARACTER SET = utf8mb4 COLLATE = utf8mb4_unicode_ci ROW_FORMAT = DYNAMIC;

-- ----------------------------
-- Records of meal_item
-- ----------------------------
INSERT INTO `meal_item` (`id`, `source_type`, `owner_user_id`, `name`, `meal_time`, `mood`, `scene`, `health_goal`, `cuisine`, `taste`, `convenience`, `created_at`, `updated_at`) VALUES
(1, 'PUBLIC', NULL, 'Tomato Egg Noodles',
 '["Lunch", "Dinner", "Three Meals"]', '["Tired", "Down"]', '["Work", "Campus", "Home"]',
 '["Light", "Stomach-Friendly", "Easy Digest"]', '["Home Style", "Noodles"]', '["Light", "Tomato"]',
 '["Quick", "Solo Meal"]', NOW(), NOW()),
(2, 'PUBLIC', NULL, 'Clear Broth Wontons',
 '["Breakfast", "Lunch", "Dinner", "Three Meals"]', '["Tired", "No Appetite"]', '["Work", "Campus", "Home"]',
 '["Light", "Stomach-Friendly", "Warming"]', '["Street Food", "Porridge & Soup"]', '["Light", "Savory"]',
 '["Quick", "Minimal Utensils"]', NOW(), NOW()),
(3, 'PUBLIC', NULL, 'Chicken Breast Light Bowl',
 '["Lunch", "Dinner"]', '["Calm", "Want To Relax"]', '["Work", "Post-Workout"]',
 '["Fat Loss", "High Protein", "Low Oil", "Balanced"]', '["Light Meal"]', '["Light", "Savory"]',
 '["Quick", "Solo Meal"]', NOW(), NOW()),
(4, 'PUBLIC', NULL, 'Spicy Stir-Fry Pot',
 '["Lunch", "Dinner", "Late Night Snack"]', '["Happy", "Treat Myself"]', '["Weekend", "Gathering", "Late Night"]',
 '["Balanced", "Energy Boost"]', '["Sichuan", "Street Food"]', '["Numbing Spicy", "Smoky"]',
 '["Slow Enjoy", "Sharing"]', NOW(), NOW()),
(5, 'PERSONAL', 1, 'Potato Braised Beef',
 '["Dinner"]', '["Calm"]', '["Campus"]',
 '["Energy Boost"]', '["Hunan"]', '["Spicy"]',
 '[]', NOW(), NOW());

-- ----------------------------
-- Table structure for recommend_feedback
-- ----------------------------
DROP TABLE IF EXISTS `recommend_feedback`;
CREATE TABLE `recommend_feedback` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `user_id` bigint NOT NULL,
  `session_id` varchar(64) NOT NULL,
  `item_id` bigint NULL DEFAULT NULL,
  `action` varchar(32) NOT NULL,
  `rating` int NULL DEFAULT NULL,
  `reason` varchar(512) NULL DEFAULT NULL,
  `created_at` datetime NOT NULL,
  PRIMARY KEY (`id`),
  INDEX `idx_feedback_user` (`user_id`, `created_at`)
) ENGINE = InnoDB CHARACTER SET = utf8mb4 COLLATE = utf8mb4_unicode_ci ROW_FORMAT = DYNAMIC;

SET FOREIGN_KEY_CHECKS = 1;
