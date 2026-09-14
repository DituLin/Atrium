# NAS 读取恢复排查与修复

2026-09-14，回应用户“NAS 不是可以正常读取吗”。结论：本次已恢复并验证实际目录读取；之前仅依据 stale/degraded 诊断将 NAS 持续视作不可读的说法不准确。

## 分层证据

1. 排查初始，挂载仍为 SMB，Core fd30e88 报 degraded/stuck_io；独立 Python stat 成功，但 scandir 5 秒未返回。
2. 本次定向 TCC 日志明确出现 Codex 的 SystemPolicyNetworkVolumes 授权等待（21:04:39），21:05:06 返回允许；随后查询也返回允许。本次命令行阻塞与待处理网络卷授权对应。没有由代理修改 TCC 数据库、绕过权限或点击授权；不能凭此反推历史每一次 Core 阻塞都是同一权限原因。
3. Finder 打开同一授权根并显示现有 MP4；授权后独立目录枚举及视频原文件读取成功。连续三次枚举 6 个根条目，并读取 65,536 字节，进程总耗时 24.80 / 20.28 / 17.75 毫秒。视频 ffprobe 得到 H.264 1920×1080、AAC、239.005438 秒、601,338,247 字节；这只证明样本读取与元数据解析，不是 TV 播放兼容验收。
4. 此时 Core 仍保持 degraded，但进程采样已不再停在 open；源码确认 stuck_ops 是累计超时计数，不是当前挂起操作数。此前将 8 解读为仍有 8 个阻塞读取，需要更正。

## Atrium 缺陷与修复

`Manager.Probe → CheckIdentity` 对根和 marker 使用普通 Stat，而普通 I/O 达到容量上限后被 degraded 门禁拒绝。恢复探测因此也无法通过，成功发布后的 ClearDegraded 永远无法执行。已有 ProbeStat 预留容量没有接入实际 Manager 路径。

修复 `dbbca5a`：根与身份 marker 的检查走保留探测槽，保留路径、符号链接、挂载身份和授权发布规则；不更换来源身份、不扩大授权目录。并发审查还发现 do 在释放槽前发送完成结果，连续探测可能撞到前一步未释放的槽；改为真实 syscall 返回后先释放容量，再发送完成通知，未返回的 syscall 仍占用名额。

两个失败回归已复现后修复：真实 OSFS FIFO 耗尽→解除阻塞→实际 Manager.Probe 恢复；连续身份检查完成后容量必须已释放。包含 marker 缺失继续拒绝的检查。独立审查接受；最终 `make check` 与 source/indexer/httpapi 的 race 全通过。没有 Web 行为、APK、挂载、Mac 电源配置变更。

## 部署与真实扫描

干净 dbbca5a 构建部署，健康版本 `dbbca5a+dbbca5a`。备份 `~/Atrium/iteration-20260912/rollback-nas-recovery-20260914/` 包含旧程序、配置及 SQLite 在线备份。部署前核对旧 fd30e88 哈希和健康状态；配置与挂载均未改变。

重启后的定时扫描 `01M2G0N8MF4FFESQEHDNB16W1H` 于 13:10:46Z 开始、13:11:30Z 完成，files_seen=17,234、files_new=62、files_changed=0、files_missing=0、files_removed=0、errors=0；数据库同样有 17,234 条部署后的实际观测。不是仅依据 stat/健康灯宣布恢复。历史降级状态下的“不重启恢复”由真实 OSFS Manager 回归证明；生产此次恢复包含版本部署重启，不能声称生产上未重启恢复已验证。

私有证据：`~/Atrium/iteration-20260912/nas-recovery-20260914/` 的 TCC 日志、采样、direct-read-results.json、video-metadata.json、race-final.log、make-check-final.log、release-build.log、deployed-build.json、diag-after.json、scan-result.json、diag-final.json。

图片和视频继续混放在 `/Volumes/home/Photos`，未写入 NAS 测试文件。当前可继续 M4 视频样本与播放器验证；单次恢复不替代长期稳定性、NAS 重连矩阵或正式 TV 验收。Mac 断电恢复、NAS 自动挂载仍暂停。
