# ADMS integration notes

How `essl_integration_service` talks to eSSL devices, what is assumed, and how to check it on a
real **eSSL AI Face Magnum**.

eSSL publishes no ADMS ("PUSH") specification; their supported route is the eBioserverNew
middleware. eSSL hardware is ZKTeco-made, so this service implements ZKTeco's PUSH 2.x protocol
as it is widely implemented. Everything marked **ASSUMED** must be confirmed on the device.

## Device setup

On the device: **Menu → COMM → Cloud Server Setting**

| Setting | Value |
|---|---|
| Server mode | ADMS |
| Server address | the service's host (local POC: the laptop's LAN IP) |
| Server port | `8001` locally; `80`/`443` behind the reverse proxy |
| HTTPS | off locally; ASSUMED: check whether the Magnum firmware supports it |
| Proxy | off |

Then register the serial number (Menu → System Info → Device Info, or the label) in the portal
under Settings → Devices. Until it is registered every request is refused with 401, but still
recorded in `device_raw_messages`, so a mistyped serial shows up there.

## Endpoints

All under `/iclock`, serial in `?SN=`. Replies are plain text.

| Request | Meaning | Reply |
|---|---|---|
| `GET /iclock/cdata?options=all` | Handshake on boot | Options: upload stamps, `Delay=10`, `Realtime=1`, `TransFlag` |
| `POST /iclock/cdata?table=ATTLOG&Stamp=N` | Punches | `OK: <lines>` after storing; stamp saved |
| `POST /iclock/cdata?table=OPERLOG&Stamp=N` | Users, templates, operations | `OK: <lines>`; only kept in the raw log for now |
| `GET /iclock/getrequest` | Heartbeat / command poll | `C:<id>:<command>` lines, or `OK` |
| `POST /iclock/devicecmd` | Command results, `ID=..&Return=..&CMD=..` per line | `OK` |
| anything else (`fdata`, `registry`, `push`, `ping`) | Not handled yet | `OK`, recorded in the raw log |

A failure to store an upload answers 503, so the device keeps the batch and retries.

## Assumptions to confirm (device spike)

The step-by-step session with a real device is in [device-test-runbook.md](device-test-runbook.md).

1. **Handshake.** The Magnum calls `GET /iclock/cdata?options=all` (PUSH 2.x), not the PUSH 3.x
   `/iclock/registry` + `/iclock/push` flow. Look at the first raw rows after setup.
2. **ATTLOG fields.** `PIN \t YYYY-MM-DD HH:MM:SS \t status \t verify \t workcode …`, local time
   with no offset.
3. **Verify codes.** 0 password, 1 fingerprint, 2/4 card, 15 face. Punch once with each method
   and compare `verifyCode` in `access_events`.
4. **Clock.** `TimeZone` is not sent in the handshake. Check the device clock matches IST and
   whether the device sets it from the server.
5. **Stamps.** After a reboot the device resumes from `ATTLOGStamp` instead of resending everything.
6. **Commands.** `CONTROL DEVICE 01010105` opens the door, `INFO` and `CHECK` are answered on
   `/iclock/devicecmd` with `Return=0`, `REBOOT` reboots,
   `DATA QUERY ATTLOG StartTime=..\tEndTime=..` re-sends punches.
7. **Refused attempts.** How an expired or unknown face is reported. ATTLOG carries accepted
   punches only; refusals may come as OPERLOG, `rtlog`, or photos on `fdata`.
8. **User sync.** `DATA UPDATE USERINFO PIN=..\tName=..\tPri=0\tPasswd=\tCard=\tGrp=1\tTZ=0000000000000000`
   creates the user, `DATA DELETE USERINFO PIN=..` removes them (and their templates on the
   device). Check both are answered with `Return=0` and take effect.
8a. **Templates.** After enrolling a face at the device, an OPERLOG upload carries `FACE`,
   `BIODATA` or `BIOPHOTO` lines for that pin. Check they arrive, and that replaying one as
   `DATA UPDATE <line>` after a delete makes the face match again without enrolling.
8b. **Validity dates** (`ADMS_SEND_VALIDITY=true`): whether `StartDatetime` / `EndDatetime` on
   USERINFO are accepted and make the device refuse an expired user while offline.
9. **HTTPS** support and the device's behaviour on certificate errors.

## Reading the raw log

Turn on `ADMS_LOG_HEARTBEATS=true` for the session so empty polls are kept too, then:

```sql
-- Everything one device said, newest first
SELECT "createdAt", "method", "path", "query", "status", LEFT("body", 300) AS body, LEFT("response", 200) AS response
  FROM device_raw_messages
 WHERE UPPER("serialNumber") = UPPER('<serial>')
 ORDER BY id DESC LIMIT 50;

-- Serials that tried to connect without being registered
SELECT "serialNumber", "remoteIp", MAX("createdAt") AS last_try, COUNT(*)
  FROM device_raw_messages WHERE "deviceId" IS NULL
 GROUP BY 1, 2 ORDER BY last_try DESC;

-- Paths the service does not handle yet
SELECT "path", COUNT(*) FROM device_raw_messages GROUP BY 1 ORDER BY 2 DESC;
```

Rows older than 30 days can be removed with `database.RawDb.PurgeRawMessages`.

## Door access (expiry)

Members are kept on the gym's devices by a reconciler (`services/access_sync_service.go`):

- For each member × device, the **desired** state comes from the membership: on the device
  while the latest term's end date plus the gym's grace days has not passed, otherwise blocked;
  a gym with *block on expiry* off keeps everyone on. Deleted members are removed.
- The **actual** state changes only when the device acknowledges a command, so a device that is
  offline stays "syncing" until it catches up.
- **Block = remove the user** from the device (a device cannot match a face it does not have).
  **Unblock = create the user again and replay their saved templates**, captured from the
  device's OPERLOG upload when they enrolled, so renewing does not mean enrolling again.
- It runs right after adding, renewing, editing or deleting a member, after gym settings change,
  when a device is added, and every 5 minutes for every gym (expiries, and anything an outage
  left behind). Only the latest state per member and device is sent: queued commands are
  superseded, so a device offline through an expiry and a renewal only receives the renewal.
- A command the device refuses is retried on each sweep up to 5 times; then the member shows
  **error** in the members list until something changes.
- Names are stripped of tabs and newlines (they separate fields and commands) and cut to 24
  characters. Members are always ordinary users (`Pri=0`), never device admins.

**Device admins must use pins from 9000 up.** Member pins start at 1, and the reconciler would
overwrite a device user that has a member's pin.

Until the device confirms ATTLOG refusals (item 7), a refused member leaves no check-in; a
member the device still let in shows as *allowed_but_expired*, meaning a block has not landed.

## Verdicts

Each punch is judged against the member's terms on the calendar day it happened (gym timezone),
not on today, so a late-uploaded backlog is judged correctly.

| Verdict | Meaning |
|---|---|
| `allowed` | A term (plus grace days) covers the punch day |
| `allowed_but_expired` | The device let in a member with no covering term: it has not been told to block them yet |
| `unknown` | The pin matches no member of this gym (e.g. an admin enrolled only on the device) |
| `denied_*` | Reserved for refusals once item 7 is confirmed |
