# HA IoT deployment and connection

[中文](ha-iot-deployment.md)

## Delivery status

As of 2026-11-04, HA and the official Xiaomi integration are deployed on the NAS. Readings and entity identifiers have been inspected for the three initial device types. Token authentication using a dedicated non-administrator account and API reads for all three device types have been verified. The candidate adapter completed a CA-verified WSS full synchronization against real HA, with all 13 capabilities available. An administrator confirmed retention of the TLS configuration. Persistence integration, TV presentation and full recovery acceptance remain incomplete. Imported devices do not imply available entities; static readings do not establish continuous updates.

Verified versions: DSM 7.4.1-90080 Update 1, Container Manager 24.0.2-1706, Docker Engine 24.0.2, HA 2026.10.4 and official Xiaomi integration v0.5.0. Inspected hardware: DS220+ with 10 GB RAM. This is not a compatibility guarantee for other models or future releases.

## Deployment boundaries

- HA owns device protocols and vendor authentication. Mount a NAS-local configuration directory at `/config`; IoT data does not travel through SMB.
- Atrium runs on the Mac. TVs connect only to Atrium. Photo shares are independent of HA.
- Use the [digest-pinned Compose example](../../deploy/iot/compose.example.yaml). Initial cloud access requires no USB, D-Bus or privileged permissions.
- Restrict configuration-directory access. Never commit authentication files, real entity inventories, accounts or screenshots. `/srv/ha` in examples is a placeholder.
- Configuration backups include HA configuration and deployment descriptions, not photo shares, media or entire volumes. Backups contain secrets and require controlled storage.
- Keep MiGPT Next stopped. Deploying HA must not restart other previously stopped containers.

## Preparing Atrium

1. Create a dedicated non-administrator HA account limited to local access. An ordinary account is not a read-only credential; the Atrium client must send only read messages.
2. Generate a long-lived token with that account and save it directly in a private Mac file, such as `/private/atrium/ha.token`, with mode 0600. Do not send it through chat or embed it in YAML.
3. Establish an HTTPS/WSS endpoint and trust chain using a trusted reverse proxy or native HA TLS. The certificate must match the accessed hostname/IP. When using `ca_file`, add the designated CA to the system trust roots without disabling certificate validation.
4. Read real IDs, units and attributes from HA entity details. Map thermometer temperature/humidity/battery, humidifier power/humidity/temperature and aquarium water temperature/pump/filter readings in private configuration. Never guess entity IDs from nicknames.
5. Verify unknown, unavailable, off and valid zero states, and distinguish source update time from latest synchronization time. Do not imply a recent measurement without actual sampling evidence.
6. After candidate tests pass, back up the Atrium database and deploy using the [release procedure](deployment-release.en.md). A new schema prevents direct rollback to an incompatible binary; restore a compatible database as part of rollback.

HA 2026.10.4 manages HTTP/TLS settings through Settings → System → Network. Changes restart HA and require administrator confirmation within a limited time. Recheck target-version documentation before deployment.

## Acceptance and rollback

- Observe real updates from at least two device types on the TV. Measure device-to-HA and HA-to-TV latency separately; the latter target is at most five seconds.
- Verify HA disconnection/reconnection, Atrium restart, removal of configured devices and revoked screen authorization. HA failure must not disable photo or archive services.
- IoT defaults off. Disable the integration configuration when necessary, preserving NAS devices and data; respect database schema rollback guards.
- HA downgrades require matching configuration backups. DSM in-place downgrade is not promised. Mac startup after login does not establish unattended recovery. Automatic photo-share remount after NAS restart requires separate acceptance.

## References

- [HA Container installation](https://www.home-assistant.io/installation/linux/)
- [HA HTTP/TLS configuration](https://www.home-assistant.io/integrations/http/)
- [HA WebSocket API](https://contributors.home-assistant.io/core/internals/websocket-api/)
- [Official Xiaomi integration](https://github.com/XiaoMi/ha_xiaomi_home)
