DROP INDEX IF EXISTS moderation.moderation_case_history_case_created_idx;
DROP INDEX IF EXISTS moderation.moderation_case_reports_case_attached_idx;
DROP INDEX IF EXISTS moderation.moderation_cases_subject_type_created_idx;
DROP INDEX IF EXISTS moderation.moderation_cases_priority_created_idx;
DROP INDEX IF EXISTS moderation.moderation_cases_status_created_idx;
DROP INDEX IF EXISTS moderation.moderation_cases_created_idx;
DROP INDEX IF EXISTS moderation.moderation_case_reports_report_id_uidx;

DROP TABLE IF EXISTS moderation.case_history;
DROP TABLE IF EXISTS moderation.case_reports;
DROP TABLE IF EXISTS moderation.cases;
