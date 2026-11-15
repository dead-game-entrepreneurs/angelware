-- Existing installations contain WWE data. Assign it explicitly before removing defaults.
ALTER TABLE accounts ADD COLUMN game_id text NOT NULL DEFAULT 'svr2007';
ALTER TABLE accounts DROP CONSTRAINT accounts_pkey, DROP CONSTRAINT accounts_name_key;
ALTER TABLE accounts ADD PRIMARY KEY (game_id, user_id), ADD UNIQUE (game_id, name);
ALTER TABLE accounts ALTER COLUMN game_id DROP DEFAULT;

ALTER TABLE stats ADD COLUMN game_id text NOT NULL DEFAULT 'svr2007';
ALTER TABLE stats DROP CONSTRAINT stats_pkey;
ALTER TABLE stats ADD PRIMARY KEY (game_id, user_id, board);
ALTER TABLE stats ALTER COLUMN game_id DROP DEFAULT;
ALTER TABLE stats ADD COLUMN extra bytea;

ALTER TABLE mailbox ADD COLUMN game_id text NOT NULL DEFAULT 'svr2007';
ALTER TABLE mailbox ALTER COLUMN game_id DROP DEFAULT;
DROP INDEX mailbox_to_id;
CREATE INDEX mailbox_to_id ON mailbox (game_id, to_id, id);

ALTER TABLE friend_requests ADD COLUMN game_id text NOT NULL DEFAULT 'svr2007';
ALTER TABLE friend_requests DROP CONSTRAINT friend_requests_pkey;
ALTER TABLE friend_requests ADD PRIMARY KEY (game_id, from_id, to_id);
ALTER TABLE friend_requests ALTER COLUMN game_id DROP DEFAULT;

ALTER TABLE friends ADD COLUMN game_id text NOT NULL DEFAULT 'svr2007';
ALTER TABLE friends DROP CONSTRAINT friends_pkey;
ALTER TABLE friends ADD PRIMARY KEY (game_id, user_id, friend_id);
ALTER TABLE friends ALTER COLUMN game_id DROP DEFAULT;

ALTER TABLE blocks ADD COLUMN game_id text NOT NULL DEFAULT 'svr2007';
ALTER TABLE blocks DROP CONSTRAINT blocks_pkey;
ALTER TABLE blocks ADD PRIMARY KEY (game_id, user_id, blocked_id);
ALTER TABLE blocks ALTER COLUMN game_id DROP DEFAULT;

ALTER TABLE storage_files ADD COLUMN game_id text NOT NULL DEFAULT 'svr2007';
ALTER TABLE storage_files ALTER COLUMN game_id DROP DEFAULT;
CREATE INDEX storage_files_game_owner ON storage_files (game_id, owner, id);

-- Game codecs own the payload format. WWE version 1 uses JSON with base64 blobs.
ALTER TABLE profiles RENAME TO legacy_profiles;
CREATE TABLE profiles (
 game_id text NOT NULL,
 user_id bigint NOT NULL,
 kind text NOT NULL,
 version integer NOT NULL CHECK (version > 0),
 payload bytea NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (game_id, user_id, kind)
);
INSERT INTO profiles (game_id, user_id, kind, version, payload, updated_at)
 SELECT 'svr2007', user_id, 'public', 1,
 convert_to(json_build_object('A', coalesce(public_a, 0), 'B', coalesce(public_b, 0),
 'Data', replace(encode(public_data, 'base64'), E'\n', ''))::text, 'UTF8'), updated_at
 FROM legacy_profiles WHERE public_data IS NOT NULL;
INSERT INTO profiles (game_id, user_id, kind, version, payload, updated_at)
 SELECT 'svr2007', user_id, 'private', 1, convert_to(array_to_json(private)::text, 'UTF8'), updated_at
 FROM legacy_profiles WHERE private IS NOT NULL;
DROP TABLE legacy_profiles;
