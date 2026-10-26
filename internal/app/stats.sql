WITH team_tasks AS (
    SELECT id, status, assignee_id, created_at, closed_at
    FROM tasks
    WHERE team_id = ?
), status_counts AS (
    SELECT status, COUNT(*) AS amount
    FROM team_tasks
    GROUP BY status
), top_assignees AS (
    SELECT u.id AS user_id, u.name, COUNT(*) AS amount
    FROM team_tasks t
    JOIN users u ON u.id = t.assignee_id
    WHERE t.status = 'done'
      AND t.closed_at >= DATE_SUB(UTC_TIMESTAMP(6), INTERVAL 30 DAY)
      AND t.closed_at <= UTC_TIMESTAMP(6)
    GROUP BY u.id, u.name
    ORDER BY amount DESC, u.id
    LIMIT 3
), comment_counts AS (
    SELECT t.id AS task_id, COUNT(c.id) AS amount
    FROM team_tasks t
    LEFT JOIN task_comments c ON c.task_id = t.id
    GROUP BY t.id
), summary AS (
    SELECT AVG(CASE WHEN status = 'done'
               THEN TIMESTAMPDIFF(MICROSECOND, created_at, closed_at) / 1000000.0 END) AS average_seconds
    FROM team_tasks
)
SELECT 'status' AS kind, status AS label, NULL AS entity_id, amount, NULL AS average_seconds
FROM status_counts
UNION ALL
SELECT 'assignee', name, user_id, amount, NULL FROM top_assignees
UNION ALL
SELECT 'comments', NULL, task_id, amount, NULL FROM comment_counts
UNION ALL
SELECT 'summary', NULL, NULL, (SELECT COALESCE(SUM(amount), 0) FROM comment_counts), average_seconds FROM summary;
