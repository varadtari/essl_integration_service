# Device test runbook: eSSL AI Face Magnum

A half-day session that checks the service against a real AI Face Magnum. Everything here has
been built and tested against `cmd/devsim`, but the ADMS command forms are assumptions until a
real device confirms them (see [adms-protocol.md](adms-protocol.md)). The session runs on the
local development setup; the service records every exchange with the device, so anything that
differs can be fixed afterwards from the log.

## Before you start (about 15 minutes)

**Bring:** the AI Face Magnum with its 12 V power supply, a network cable (or Wi-Fi on the same
network as the laptop), and the device's admin password. A door lock is optional: without one
you can still hear the relay click on "Open door".

**On the laptop**

1. Start the development environment: database, backend and portal (see the README).
2. In `.env`, set `ADMS_LOG_HEARTBEATS=true` so every exchange is kept, then restart the backend.
3. Find the laptop's LAN IP: `hostname -I`.
4. If the firewall is on, let the device reach the backend: `sudo ufw allow 8001/tcp`.
5. Open DBeaver (or `psql`) on the `najah_local` database (localhost:55432).

**On the device**

6. **Menu → System Info → Device Info**: write down the **serial number** and **firmware version**.
7. **Enrol yourself as an admin with user ID 9001.** This keeps you from being locked out.
   Admins must use IDs from 9000 up: lower IDs belong to members and the service manages them.
8. Check the device's **date and time** are correct in IST.

## Results table

Fill this in as you go.

| # | Test | Pass? | Notes |
|---|---|---|---|
| 1 | Device connects | | first path seen: |
| 2 | Members pushed to the device | | |
| 3 | Face enrolment uploads a template | | template kind: |
| 4 | Punch with face / fingerprint / card / PIN | | verify codes: |
| 5 | Check-in time is correct | | |
| 6 | Expired member removed, door refuses them | | how the refusal is reported: |
| 7 | Renewal restores them, face works without re-enrolling | | |
| 8 | Offline backlog and reboot, no duplicates | | |
| 9 | Commands: open door / re-pull / reboot / sync | | |
| 10 | (optional) Validity dates | | |

## The tests

### 1. Connect the device

1. Portal → **Settings → Add a device** with the serial from the label; name it "Front door".
2. On the device, **Menu → COMM → Cloud Server Setting**:

   | Setting | Value |
   |---|---|
   | Server mode | ADMS |
   | Server address | the laptop's IP |
   | Port | `8001` |
   | HTTPS | off |
   | Proxy | off |

3. Save and reboot the device.

✅ **Pass:** within about a minute the device card in Settings shows **Online**.

❌ **If not:**

```sql
SELECT "createdAt", "serialNumber", method, path, query, status
FROM device_raw_messages ORDER BY id DESC LIMIT 20;
```

- **No rows:** the device is not reaching the laptop. Ping the device from the laptop and use the
  device's Network Diagnosis menu.
- **Rows with status 401:** the serial does not match. Compare `serialNumber` with the one
  registered in the portal.
- **Note the first `path`.** It should be `/iclock/cdata` with `options=all` in the query. If it is
  `/iclock/registry` or `/iclock/push`, the device uses a newer protocol version: record it.

### 2. Members go onto the device

Within about 10 seconds of connecting, the active members are sent to the device.

On the device: **Menu → User Mgt → All Users**.

✅ **Pass:** the active members appear with the **same PIN and name** as in the portal, and the
overdue ones are **not** there. Add a new member in the portal: they appear on the device within
about 10 seconds.

```sql
SELECT id, kind, status, "returnCode", LEFT(command, 80) AS command
FROM device_commands ORDER BY id DESC LIMIT 20;
```

✅ `sync_user` rows are `acked` with `returnCode` 0.

### 3. Enrol a face

On the device: **User Mgt → pick a member (e.g. PIN 2) → Face → enrol**.

✅ **Pass:**

```sql
SELECT m.name, t.kind, t.slot, LENGTH(t.line) AS bytes
FROM biometric_templates t JOIN members m ON m.id = t."memberId";
```

shows a row for that member. Write down the `kind` (FACE, BIODATA or BIOPHOTO).

❌ **If there is no row,** look in `device_raw_messages` for a request with `table=OPERLOG` in its
query and record what its body contains.

### 4. Punch with every method

One punch each with face, fingerprint, card and keypad PIN, using members enrolled that way.

✅ **Pass:** each appears in the dashboard's **Live check-ins** within a couple of seconds, with
the right method.

```sql
SELECT "punchedAt", pin, "verifyCode", "verifyMethod", verdict, raw
FROM access_events ORDER BY id DESC LIMIT 10;
```

Write down the `verifyCode` for each method. A method shown as "Other" needs mapping in
`services/adms_codec.go` (`verifyMethod`).

### 5. Time

✅ **Pass:** the check-in time in the portal matches the device's clock and your watch.

❌ Off by exactly 5 hours 30 minutes means a timezone setting; record which way.

### 6. Expiry blocks the door (the most important test)

1. Pick a member with an enrolled face. In DBeaver, set their latest `memberships."endDate"` to
   yesterday.
2. Trigger a check now instead of waiting up to 5 minutes: in **Settings → Access rules**, change
   grace days (e.g. 0 → 1, save, → 0, save).

✅ **Pass:**

- the member shows **Blocked** in the portal's Members list;
- they have **disappeared** from the device's user list;
- their face **no longer opens the door**.

🔎 **Record what the device does on the refused attempt:** what it shows on screen, and whether a
new row appears in `device_raw_messages` (look for `OPERLOG`, `rtlog` or `fdata`). This decides how
the dashboard can show "Denied – expired".

### 7. Renewal restores the face

In the portal, **Renew** that member.

✅ **Pass:** they are back on the device within about 10 seconds and **their face works without
enrolling again**.

❌ **If they are back but the face is not recognised,** the template restore needs adjusting
(`services/adms_users.go`). Record it, and enrol them again to carry on.

### 8. Network drop and reboot

1. Unplug the network cable (or turn Wi-Fi off). Punch 5 times with different members.
2. Wait 3 minutes: the dashboard shows the red **offline** banner.
3. Plug the network back in.

✅ **Pass:** all 5 punches arrive with their **original** times and the banner clears.

4. Reboot the device.

✅ **Pass:** no duplicate check-ins appear afterwards (compare the count before and after).

```sql
SELECT COUNT(*) FROM access_events;
```

### 9. Commands

From the device card in **Settings**:

| Button | ✅ Pass |
|---|---|
| **Open door** | the relay clicks or the lock opens; history shows *Done* |
| **Re-pull last 24 h** | history shows *Done*; **no** duplicate check-ins |
| **Reboot** | the device restarts and comes back Online |
| **Sync members** | members are sent again; all *Done* |

⚠️ If a real lock is wired, **Open door really unlocks it**.

### 10. Optional: the device enforces expiry itself while offline

1. Set `ADMS_SEND_VALIDITY=true` in `.env`, restart the backend, and press **Sync members**.

   ✅ The `sync_user` commands are still `acked`. If they now **fail**, set it back to `false` and
   press Sync members again: the device does not support validity dates, which is worth knowing.

2. If they were acknowledged: unplug the network, make a member expire in the database (the
   device is not told, being offline), and wait until the day after their end date (or use a
   member whose end date is today, and test after midnight).

   ✅ The device refuses them on its own.

## Afterwards, send

1. The filled-in **results table**.
2. An export of **`device_raw_messages`** (DBeaver: right-click the table → Export Data → CSV).
3. **Photos** of any error messages on the device screen.
4. The **firmware version** from step 6 of the preparation.

Most fixes are small: the protocol details live in `services/adms_codec.go` and
`services/adms_users.go`. After a fix, re-run only the failed rows.

## If something goes badly wrong

If members you did not expect are removed, or the device misbehaves, turn off **Settings →
Access rules → Block entry when a membership expires**. While it is off, nobody is blocked and
every member is put back on the device.
