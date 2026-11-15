-- Credentials belong to the unified service. Refuse to silently choose between
-- conflicting accounts from an installation that used per-game credentials.
DO $$ BEGIN
 IF EXISTS (
  SELECT user_id FROM accounts GROUP BY user_id
  HAVING count(DISTINCT name) > 1 OR count(DISTINCT password_tiger) > 1
 ) THEN
  RAISE EXCEPTION 'Conflicting per-game credentials require explicit resolution before account unification';
 END IF;
END $$;
ALTER TABLE accounts RENAME TO game_accounts;
CREATE TABLE accounts (
 user_id bigint PRIMARY KEY,
 name text NOT NULL UNIQUE,
 password_tiger bytea NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 last_login timestamptz
);
INSERT INTO accounts (user_id,name,password_tiger,created_at,last_login)
 SELECT user_id,name,password_tiger,min(created_at),max(last_login)
 FROM game_accounts GROUP BY user_id,name,password_tiger;
DROP TABLE game_accounts;
