# Deployment and upgrades

Build and test the release before interrupting the running service. The legacy environment
configuration still starts WWE and CoD BRO. Enabling OutRun requires the structured game
configuration; see [network configuration](architecture.md#network-configuration) and the
[OutRun patch setup](../patches/outrun2006/README.md). Family Feud also requires structured
configuration. WSOP 2008 uses structured configuration as well. The
[single-IP example](../configs/single-ip.example.json) shares both games' SDK transport
with WWE. Redirect Family Feud's four title-specific names, WSOP's two MMP3 names,
and the four generic SDK discovery names listed in the README. WSOP adds no new
listening ports or database migrations.

## Upgrade an existing installation

1. Record the running image ID and Git revision, preserve the current `.env` and game JSON
   privately, and tag the old image for rollback. Keep these files outside Git.
2. Fetch the intended commit with a clean working tree and build the production image.
   Hosted beta installations must retain `compose.beta.yaml` and the capture service.
3. Stop only `angelware` to prevent writes, then take a custom-format PostgreSQL backup:

   ```sh
   umask 077
   mkdir -p work/backups
   docker compose stop angelware
   docker compose exec -T postgres pg_dump -U angelware -d angelware -Fc > work/backups/before-upgrade.dump
   docker compose exec -T postgres pg_restore --list < work/backups/before-upgrade.dump > /dev/null
   ```

4. Start the built image with `docker compose up -d --no-build angelware capture` in beta
   mode (omit `capture` for the base stack). PostgreSQL and unrelated game services stay up.
5. Verify the listeners, discovery replies, auth/lobby routing and capture health. Check
   migration version 3 and the preserved account/profile/stat/file counts. A server smoke
   test does not replace a retail console/emulator test against the hosted address.

Migrations are transactional and serialize through a PostgreSQL advisory lock. Existing WWE
credentials become shared accounts; its profiles, stats, friends, messages and files remain
scoped to `svr2007`. The upgrade does not import a development database or test accounts.

## Rollback

An old binary cannot use a schema newer than it supports. Keep the matching database backup, image and
configuration together. If the rollout changed the schema, stop the new application, restore the complete
pre-upgrade database into a clean database, restore the saved configuration and run the saved
image. Merely switching the image back is insufficient after a schema change. For a release
that keeps the existing schema, restore its image and configuration without replacing current
gameplay data. Restore operations replace newer writes, so preserve an additional incident
backup first if the new server accepted gameplay.

## Diagnostics

For structured configuration, set `log_level` in the game JSON to `debug` for a hosted beta.
Set `ANGELWARE_CAPTURE_EXTRA_UDP_PORTS=3658` when OutRun's native relay is enabled. The
[diagnostics runbook](beta-diagnostics.md) describes retention and private incident bundles.
Raw captures, `.env` files, database dumps and recovered ticket/key material stay private.
