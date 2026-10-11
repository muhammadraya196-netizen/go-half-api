-- name: GetUserAuth :one
SELECT id, name, email, role, status, password_hash
FROM users
WHERE lower(email) = lower(sqlc.arg('email'));

-- name: TouchLastLogin :exec
UPDATE users
SET last_login = now()
WHERE id = sqlc.arg('id');

-- name: ListUsers :many
SELECT id, name, username, email, phone, role, status, avatar, created_at, last_login
FROM users
WHERE
  (
    sqlc.narg('search')::text IS NULL
    OR name ILIKE sqlc.narg('search')
    OR username ILIKE sqlc.narg('search')
    OR email ILIKE sqlc.narg('search')
  )
  AND (sqlc.narg('role')::text IS NULL OR role = sqlc.narg('role'))
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('start_at')::timestamptz IS NULL OR created_at >= sqlc.narg('start_at'))
  AND (sqlc.narg('end_before')::timestamptz IS NULL OR created_at < sqlc.narg('end_before'))
  AND (
    sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR (created_at, id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::text)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.narg('row_limit')::integer
OFFSET sqlc.arg('row_offset')::integer;

-- name: CountUsers :one
SELECT COUNT(*)::bigint AS total
FROM users
WHERE
  (
    sqlc.narg('search')::text IS NULL
    OR name ILIKE sqlc.narg('search')
    OR username ILIKE sqlc.narg('search')
    OR email ILIKE sqlc.narg('search')
  )
  AND (sqlc.narg('role')::text IS NULL OR role = sqlc.narg('role'))
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('start_at')::timestamptz IS NULL OR created_at >= sqlc.narg('start_at'))
  AND (sqlc.narg('end_before')::timestamptz IS NULL OR created_at < sqlc.narg('end_before'));

-- name: GetUserWithActivities :one
SELECT
  users.id, users.name, users.username, users.email, users.phone, users.role, users.status,
  users.avatar, users.created_at, users.last_login,
  COALESCE((
    SELECT jsonb_agg(
      jsonb_build_object(
        'id', activity.id,
        'action', activity.action,
        'createdAt', activity.created_at
      )
      ORDER BY activity.created_at DESC, activity.id DESC
    )
    FROM (
      SELECT user_activities.id, user_activities.action, user_activities.created_at
      FROM user_activities
      WHERE user_activities.user_id = users.id
        AND (
          sqlc.narg('activity_cursor_at')::timestamptz IS NULL
          OR (user_activities.created_at, user_activities.id) < (
            sqlc.narg('activity_cursor_at')::timestamptz,
            sqlc.narg('activity_cursor_id')::text
          )
        )
      ORDER BY user_activities.created_at DESC, user_activities.id DESC
      LIMIT sqlc.arg('activity_limit')::integer
    ) AS activity
  ), '[]'::jsonb)::text AS activities
FROM users
WHERE users.id = sqlc.arg('id');

-- name: InsertUser :one
INSERT INTO users (
  id, name, username, email, phone, role, status, avatar, password_hash
) VALUES (
  sqlc.arg('id'),
  sqlc.arg('name'),
  sqlc.arg('username'),
  sqlc.arg('email'),
  sqlc.narg('phone'),
  sqlc.arg('role'),
  sqlc.arg('status'),
  sqlc.narg('avatar'),
  sqlc.arg('password_hash')
)
RETURNING id, name, username, email, phone, role, status, avatar, created_at, last_login;

-- name: InsertUserActivity :exec
INSERT INTO user_activities (id, user_id, action)
VALUES (sqlc.arg('id'), sqlc.arg('user_id'), sqlc.arg('action'));

-- name: UpdateUser :one
UPDATE users
SET
  name = sqlc.arg('name'),
  username = sqlc.arg('username'),
  email = sqlc.arg('email'),
  phone = sqlc.narg('phone'),
  role = sqlc.arg('role'),
  status = sqlc.arg('status'),
  avatar = sqlc.narg('avatar')
WHERE id = sqlc.arg('id')
RETURNING id, name, username, email, phone, role, status, avatar, created_at, last_login;

-- name: UpdateUserPassword :one
UPDATE users
SET password_hash = sqlc.arg('password_hash')
WHERE id = sqlc.arg('id')
RETURNING id, name, username, email, phone, role, status, avatar, created_at, last_login;

-- name: UpdateUserStatus :one
UPDATE users
SET status = sqlc.arg('status')
WHERE id = sqlc.arg('id')
RETURNING id, name, username, email, phone, role, status, avatar, created_at, last_login;

-- name: DeleteUser :execrows
DELETE FROM users
WHERE id = sqlc.arg('id');

-- name: GetUserStatus :one
SELECT id, status
FROM users
WHERE id = sqlc.arg('id');
