# Atrium 文档 / Documentation

[中文项目介绍](../README.md) · [English project overview](../README.en.md)

## 开始使用 / Getting started

- [构建、发布与部署（中文）](ops/deployment-release.md) / [Build, release and deployment (English)](ops/deployment-release.en.md)：现有 CI、完整候选包、独立受控部署、本机签名、备份与兼容回滚 / current CI, complete candidates, independent deployment, local signing, backup and compatible rollback.
- [Android TV 构建与使用 / Android TV build and usage](../android-tv/README.md)：APK、公共 CA 配置、配对和遥控器 / APK, public CA provisioning, pairing and remote controls.
- [基础服务运维 / Base service operations](ops/runbook.md) 与 [AI 运维 / AI operations](ops/ai-brain-mcp-runbook.md)：基础服务与 Brain/MCP 的详细操作 / detailed Core and Brain/MCP operations.

## 架构与交付状态 / Architecture and delivery status

- [整体系统设计 / System design](architecture/atriumos-system-design.md)
- [智能家庭档案设计 / Intelligent family archive design](plans/2026-10-03-family-agentos-archives-design.md)
- [详细开发计划 / Development plan](plans/2026-10-03-atriumos-development-plan.md)
- [当前实施与验收状态 / Implementation and acceptance status](ops/atriumos-implementation-status.md)：明确已上线能力与 T15–T18 未完成项 / deployed capabilities and outstanding T15–T18 gates.
- [历史上线与恢复记录 / Historical deployment and recovery evidence](ops/atriumos-runbook.md)
- [TV 排序与奶量趋势验证 / TV ordering and milk-trend verification](ops/evidence/2026-10-11-archive-tv-hotfix/README.md)

## 接口 / Interfaces

- [API 文档目录 / API documentation](api/)
- [基础产品需求 / Base product requirements](prd/2026-09/atrium-home-hub/prd.md)
- [基础技术设计 / Base technical design](tech/2026-09/atrium-home-hub/tech-design.md)

## 语言维护 / Language maintenance

README 以中文为主，完整英文版为 `README.en.md`。新增或实质改写的对外项目、使用、部署文档优先提供中英文，并同步技术事实、命令和状态；历史证据和已有设计文档可保留原语言，改写时再补对应版本。详见[贡献语言约定](../AGENTS.md)。

The primary README is Chinese, with a complete `README.en.md`. New or substantially rewritten public-facing project, usage and deployment documentation should provide Chinese and English with matching facts, commands and status. Historical evidence and existing designs may retain their original language until rewritten. See the [documentation language policy](../AGENTS.md).
