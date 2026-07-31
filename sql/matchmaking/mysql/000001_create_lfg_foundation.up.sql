CREATE TABLE IF NOT EXISTS `lfg_entries` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `realm_id` INT UNSIGNED NOT NULL,
    `battlegroup_id` INT UNSIGNED NOT NULL DEFAULT 0,
    `party_id` BIGINT UNSIGNED NULL,
    `leader_guid` BIGINT UNSIGNED NOT NULL,
    `queue_category` VARCHAR(64) NOT NULL,
    `selected_dungeons` JSON NOT NULL,
    `state` VARCHAR(24) NOT NULL,
    `partition_key` VARCHAR(191) NOT NULL,
    `version` BIGINT UNSIGNED NOT NULL DEFAULT 1,
    `created_at` TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    `updated_at` TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    KEY `idx_lfg_entries_matcher` (`partition_key`, `state`, `created_at`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `lfg_entry_members` (
    `entry_id` BIGINT UNSIGNED NOT NULL,
    `realm_id` INT UNSIGNED NOT NULL,
    `player_guid` BIGINT UNSIGNED NOT NULL,
    `selected_roles` TINYINT UNSIGNED NOT NULL,
    `assigned_role` TINYINT UNSIGNED NOT NULL DEFAULT 0,
    `level` TINYINT UNSIGNED NOT NULL,
    `class` TINYINT UNSIGNED NOT NULL,
    `online` BOOLEAN NOT NULL DEFAULT TRUE,
    `joined_at` TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`entry_id`, `realm_id`, `player_guid`),
    UNIQUE KEY `uq_lfg_active_player` (`realm_id`, `player_guid`),
    CONSTRAINT `fk_lfg_member_entry` FOREIGN KEY (`entry_id`) REFERENCES `lfg_entries` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `lfg_proposals` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `partition_key` VARCHAR(191) NOT NULL,
    `dungeon_id` INT UNSIGNED NOT NULL,
    `state` VARCHAR(24) NOT NULL,
    `version` BIGINT UNSIGNED NOT NULL DEFAULT 1,
    `expires_at` TIMESTAMP(6) NOT NULL,
    `created_at` TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    `updated_at` TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    KEY `idx_lfg_proposals_expiry` (`state`, `expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `lfg_proposal_members` (
    `proposal_id` BIGINT UNSIGNED NOT NULL,
    `entry_id` BIGINT UNSIGNED NOT NULL,
    `realm_id` INT UNSIGNED NOT NULL,
    `player_guid` BIGINT UNSIGNED NOT NULL,
    `assigned_role` TINYINT UNSIGNED NOT NULL,
    `response` VARCHAR(16) NOT NULL DEFAULT 'pending',
    `responded_at` TIMESTAMP(6) NULL,
    PRIMARY KEY (`proposal_id`, `realm_id`, `player_guid`),
    UNIQUE KEY `uq_lfg_proposal_entry` (`proposal_id`, `entry_id`),
    CONSTRAINT `fk_lfg_proposal_member_proposal` FOREIGN KEY (`proposal_id`) REFERENCES `lfg_proposals` (`id`) ON DELETE CASCADE,
    CONSTRAINT `fk_lfg_proposal_member_entry` FOREIGN KEY (`entry_id`) REFERENCES `lfg_entries` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `lfg_runs` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `proposal_id` BIGINT UNSIGNED NOT NULL,
    `dungeon_id` INT UNSIGNED NOT NULL,
    `instance_id` INT UNSIGNED NULL,
    `group_id` BIGINT UNSIGNED NULL,
    `gameserver_id` VARCHAR(191) NULL,
    `gameserver_address` VARCHAR(255) NULL,
    `state` VARCHAR(24) NOT NULL,
    `allocation_token` CHAR(36) NOT NULL,
    `version` BIGINT UNSIGNED NOT NULL DEFAULT 1,
    `created_at` TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    `updated_at` TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uq_lfg_run_proposal` (`proposal_id`),
    UNIQUE KEY `uq_lfg_allocation_token` (`allocation_token`),
    CONSTRAINT `fk_lfg_run_proposal` FOREIGN KEY (`proposal_id`) REFERENCES `lfg_proposals` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `lfg_partition_leases` (
    `partition_key` VARCHAR(191) NOT NULL,
    `owner_id` VARCHAR(191) NOT NULL,
    `fencing_token` BIGINT UNSIGNED NOT NULL,
    `lease_until` TIMESTAMP(6) NOT NULL,
    `updated_at` TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`partition_key`),
    KEY `idx_lfg_partition_lease_expiry` (`lease_until`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `lfg_outbox` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `aggregate_type` VARCHAR(32) NOT NULL,
    `aggregate_id` BIGINT UNSIGNED NOT NULL,
    `event_type` VARCHAR(96) NOT NULL,
    `payload` JSON NOT NULL,
    `created_at` TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    `published_at` TIMESTAMP(6) NULL,
    `attempts` INT UNSIGNED NOT NULL DEFAULT 0,
    PRIMARY KEY (`id`),
    KEY `idx_lfg_outbox_unpublished` (`published_at`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
