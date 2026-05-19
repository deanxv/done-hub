<p align="right">
   <strong>中文</strong> | <a href="./README.en.md">English</a>
</p>

<p align="center">
   <picture>
   <img style="width: 80%" src="https://pic1.imgdb.cn/item/6846e33158cb8da5c83eb1eb.png" alt="image__3_-removebg-preview.png"> 
    </picture>
</p>

<div align="center">

_本项目是基于 [one-hub](https://github.com/MartialBE/one-api) 二次开发而来的_

<a href="https://t.me/+raL5ppEzDIFmZTY1">
  <img src="https://img.shields.io/badge/Telegram-AI Wave交流群-0088cc?style=for-the-badge&logo=telegram&logoColor=white" alt="Telegram 交流群" />
</a>

<sup><i>AI Wave 社群</i></sup> · <sup><i>(群内提供公益 API、AI 机器人)</i></sup>

### [📚 点击查看原项目文档](https://one-hub-doc.vercel.app/)

</div>

---

## 项目简介

**Done Hub** 是基于 [one-hub](https://github.com/MartialBE/one-api) 的二次开发版本，在保持与原版**数据库与镜像完全兼容**的前提下，围绕**新型客户端反代（Claude Code / Gemini CLI / Codex / Antigravity 等）**、**渠道精细化管控**、**邀请与返利体系**、**数据分析**与**多实例部署稳定性**等方向做了大量增强与修复。

> 数据库与原版兼容，原版用户可直接拉取本项目镜像 `deanxv/done-hub` 完成平滑迁移。

---

## 与原版（one-hub 最新镜像）的差异概览

下面按照**新增渠道**、**原生路由兼容**、**渠道功能增强**、**邀请与返利**、**批量管理**、**数据分析**、**登录授权**、**界面交互**、**Bug 修复**、**性能与稳定性**十大维度进行梳理。

### 一、新增渠道类型（Provider）

| 渠道 | 说明 |
| --- | --- |
| **Codex**（反代） | 新增对 Codex CLI / Codex API 的反向代理渠道，可作为标准上游 Provider 接入。 |
| **Claude Code**（反代） | 新增 ClaudeCode 反代渠道，直接对接 Claude Code 客户端（含 `/v1/messages` 原生协议）。 |
| **Gemini CLI**（反代） | 新增 GeminiCli 渠道（Channel Type `57`），透传 Gemini CLI 客户端原生请求。 |
| **Antigravity**（反代） | 新增 Antigravity 渠道（Channel Type `60`），对接 `daily-cloudcode-pa.googleapis.com` 等替代端点。 |
| **Vertex AI Express** | 新增 Vertex AI Express 渠道（Channel Type `61`），相较普通 Vertex AI 提供更轻量的接入方式。 |

### 二、原生路由（Native Route）跨渠道兼容

原版 Provider 通常仅能"按自身协议"被调用，Done Hub 打通了不同协议在不同渠道之间的相互转换，使一个渠道可以同时承担多种客户端的反代角色：

- **自定义渠道**支持以 **Claude 原生路由**（`/v1/messages`）对外提供服务 —— 可直接接入 Claude Code。
- **Vertex AI** 渠道支持以 **Gemini 原生路由**（`/gemini/*`）对外提供服务 —— 可直接接入 Gemini CLI。
- **Vertex AI** 渠道支持以 **Claude 原生路由**对外提供服务 —— 可直接接入 Claude Code。
- **Vertex AI** 渠道支持配置**多个 Region**，每次请求随机挑选 Region，自动做请求级负载均衡。
- **Google Gemini** 渠道的 `/gemini` 路由支持原生**视频生成请求**（Veo 系列模型）。
- 支持 `gemini-2.0-flash-preview-image-generation` 的**文生图 / 图生图**，并同时兼容 **OpenAI Chat Completions** 接口。

### 三、渠道（Channel）功能增强

- **请求参数透传 / 过滤**
    - `/gemini` 原生生图请求支持**额外参数透传**。
    - Claude 渠道（OpenAI 格式 与 原生格式）均支持**额外参数透传**。
    - 渠道级 `remove_params` 支持**嵌套字段**删除，如 `"remove_params": ["generationConfig.thinkingConfig"]`。
- **模型映射与命名**
    - 支持配置**模型名称大小写不敏感**（避免大小写写错导致渠道找不到模型）。
    - 支持配置**请求 / 响应统一模型名称**，让上下游模型名一致。
    - 模型重定向支持"键"**自动映射**到模型配置。
- **BaseURL 模板化**
    - 渠道 `BaseURL` 支持**模型变量替换**，可基于请求模型动态拼接上游地址。
- **渠道行为开关**
    - 新增**空回复是否计费**开关（默认计费）。
    - 新增**内置聊天功能**渠道级开关。
    - 渠道级 `allow_extra_body` 控制是否放行额外请求体字段。

### 四、邀请与返利体系（新增）

- **邀请码（Invite Code）模块**
    - 支持**手动 / 自动**生成邀请码、**批量创建**（单次最多 100 条）、**批量删除**。
    - 单码可配置**最大使用次数**、**生效起止时间**、**启用 / 禁用**状态。
    - 提供邀请码使用统计与状态追踪。
- **邀请充值返利**
    - 当被邀请人充值时，可向**邀请人**返利：
        - **固定金额** 或 **百分比** 两种类型可选。
    - 与原有"邀请赠送额度"功能并存，形成完整的拉新激励链路。
- **第三方登录解耦邀请码授权**
    - 修复第三方登录与邀请码强耦合的问题，可独立配置开放策略。

### 五、批量管理

- **批量删除渠道**
- **批量为多个渠道新增模型**
- **批量为多个渠道追加用户分组**
- 邀请码 / 系统日志等场景均补齐了对应的批量操作。

### 六、数据分析与日志

- **新增 RPM / TPM / CPM 实时指标展示**。
- **充值统计**新增**时间周期条件**（全部 / 年 / 月 / 周 / 日）。
- 系统日志（`system_log`）新增条件查询与分页参数。
- 日志支持 **CSV 导出**，便于离线分析与对账。
- 删除了日志功能中无意义的"原始价格"相关样式，UI 更清爽。

### 七、登录与授权

- 新增 **LinuxDo OAuth** 登录，支持按 **Trust Level** 限制注册 / 登录（Basic / Member / Regular / Leader 可配置）。
- **第三方登录**与**邀请码**授权解耦。

### 八、界面与交互（UI / UX）

- **系统信息**模块整体重构，信息密度与可读性提升。
- 支持**夜间模式跟随系统配置**（OS 暗色 / 亮色自动切换）。
- 优化**邮箱规则校验**。
- 优化大量后台 UI 交互细节（表格排序、图标、表单、提示等）。
- 优化禁用渠道**邮件推送**逻辑，避免重复 / 误报。

### 九、Bug 修复

涵盖计费、缓存、统计、支付、安全等关键链路：

- 修复用户相关接口失效的 bug。
- 修复邀请记录字段缺失的 bug。
- 修复**时区硬编码**影响统计数据的 bug（统一遵循 `TZ` / `time.Local`）。
- 修复**更新渠道后未重新内存加载**的 bug。
- 修复**多实例部署下支付回调异常**的 bug。
- 修复智谱 **GLM 模型 token 浮点数**计算的 bug。
- 修复 API 路由下允许 **CDN 缓存**引起的越权 bug。
- 修复 **MySQL 多版本下时间类型格式化**不统一的 bug。
- 修复若干**用户额度缓存与 DB 数据不一致**导致的计费异常。
- 修复 Responses API **`cached_tokens`** 字段缺失问题。
- 修复 OpenAI 兼容 Usage 中无法解析 **Anthropic 风格 cache tokens** 的问题。
- 修复 Gemini CLI Provider 在 token 刷新 / 异常处理中的 context 传递问题。

### 十、性能与稳定性优化

- **Vertex AI 鉴权缓存**优化，显著减少重复鉴权请求。
- **Key 缓存预热**修复与优化，启动后命中率更高。
- **渠道更新即时刷新内存缓存**，无需重启。
- **重试 / 冷却（cooldown）逻辑**优化，更稳定的失败转移。
- **`/gemini` 请求中 `google_search` 响应**优化。
- 大量**日志打印格式**统一与精简，问题定位更高效。
- Alipay 支付链路重构，Epay 等通道相关 bug 修复。

---

## 部署

> 按照原版部署教程将镜像替换为 `deanxv/done-hub` 即可。

> 数据库兼容，原版可直接拉取此镜像迁移。

常见使用方式：

```bash
docker pull deanxv/done-hub:latest

docker run -d \
  --name done-hub \
  -p 3000:3000 \
  -e TZ=Asia/Shanghai \
  -v $(pwd)/data:/data \
  deanxv/done-hub:latest
```

> 完整部署、配置、计费、模型价格等参考原项目文档：<https://one-hub-doc.vercel.app/>

---

## 反馈与交流

- Telegram 交流群：[AI Wave 社群](https://t.me/+raL5ppEzDIFmZTY1)（群内提供公益 API、AI 机器人）
- Issue / PR 欢迎提交至本仓库。

## 致谢

- 本程序使用了以下开源项目
    - [one-hub](https://github.com/MartialBE/one-api) — 本项目的基础

感谢以上项目的作者与贡献者，以及社区中所有提交 PR / Issue / 反馈的小伙伴。
