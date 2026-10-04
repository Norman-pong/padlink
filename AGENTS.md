<!--VITE PLUS START-->

# Using Vite+, the Unified Toolchain for the Web

This project is using Vite+, a unified toolchain built on top of Vite, Rolldown, Vitest, tsdown, Oxlint, Oxfmt, and Vite Task. Vite+ wraps runtime management, package management, and frontend tooling in a single global CLI called `vp`. Vite+ is distinct from Vite, and it invokes Vite through `vp dev` and `vp build`. Run `vp help` to print a list of commands and `vp <command> --help` for information about a specific command.

Docs are local at `node_modules/vite-plus/docs` or online at https://viteplus.dev/guide/.

## Built-in Commands vs Scripts

`vp <name>` runs a built-in command. `vp run <name>` runs a `package.json` script or a `vite.config.ts` task. Scripts cannot overwrite built-ins, so `vp dev` and `vp run dev` may do different things. Check `package.json` and `vite.config.ts` first, and run `vp run <name>` when the project defines a script or task with that name.

## Tool Versions

Run `vp toolchain` to show versions and relationships in the active Vite+
release. Add a tool name to select part of the graph. For example, run
`vp toolchain vite`. Use `--global` to ignore the local `vite-plus` package. Use
`vp why <package>` to show the package-manager dependency graph.

## Review Checklist

- [ ] Run `vp install` after pulling remote changes and before getting started.
- [ ] Run `vp check` and `vp test` to format, lint, type check and test changes.
- [ ] Check if there are `vite.config.ts` tasks or `package.json` scripts necessary for validation, run via `vp run <script>`.
- [ ] If setup, runtime, or package-manager behavior looks wrong, run `vp env doctor` and include its output when asking for help.

<!--VITE PLUS END-->

<!--LORE PROTOCOL START-->

# Lore Protocol Integration

This project uses the **Lore protocol** (v1.0) to embed structured decision context into git commits. Lore trailers record constraints, rejected alternatives, and directives alongside code changes. You MUST query Lore before modifying files and write Lore-enriched commits.

## Before Modifying Any File

Query the Lore context for every file or directory you are about to change:

```sh
lore constraints <path> --json
lore rejected <path> --json
lore directives <path> --json
```

**Rules:**
- **Constraint** = hard requirement. Do not write code that violates it.
- **Rejected** = approach already tried and abandoned (`alternative | reason`). Do not re-explore it.
- **Directive** = standing instruction. Follow it.

If `lore constraints` returns results for a file, verify your planned changes comply before writing code. If `lore rejected` matches your intended approach, choose a different one.

## When Committing

**Convention: write commit messages in 中文 (Simplified Chinese).** The `intent` line, `body`, and all lore trailers (Constraint / Rejected / Directive / Tested / Not-tested / etc.) must be Chinese. Conventional-commit type prefixes (`feat:`, `fix:`, `chore:`, `docs:`, `refactor:`, ...) and the Lore-id / enum values (`low` / `medium` / `high` / `narrow` / `moderate` / `wide` / `clean` / `migration-needed` / `irreversible`) stay in English — only the human-readable prose is localized. Keep the `intent` ≤ 72 characters.

Stage changes with `git add`, then pipe JSON to `lore commit`:

```sh
echo '{
  "intent": "fix: 处理鉴权中间件中的空用户",
  "body": "此前遇到空用户会抛 500，现改为返回 401。",
  "trailers": {
    "Constraint": ["不得抛出异常 -- 应改为返回 401"],
    "Rejected": ["静默重定向到登录页 | 会破坏 API 客户端"],
    "Confidence": "high",
    "Scope-risk": "narrow",
    "Tested": ["空用户返回 401", "合法用户仍能正常通过"],
    "Not-tested": ["并发请求的竞态条件"]
  }
}' | lore commit
```

### JSON Schema

```json
{
  "intent": "string (REQUIRED) -- why the change was made, max 72 chars",
  "body": "string (optional) -- narrative context",
  "trailers": {
    "Constraint": ["string array -- hard requirements that must hold"],
    "Rejected": ["string array -- format: 'alternative | reason'"],
    "Confidence": "enum: 'low' | 'medium' | 'high'",
    "Scope-risk": "enum: 'narrow' | 'moderate' | 'wide'",
    "Reversibility": "enum: 'clean' | 'migration-needed' | 'irreversible'",
    "Directive": ["string array -- instructions for future maintainers"],
    "Tested": ["string array -- what was verified"],
    "Not-tested": ["string array -- known untested areas"],
    "Supersedes": ["8-char hex Lore-id array -- decisions this replaces"],
    "Depends-on": ["8-char hex Lore-id array -- decisions this requires"],
    "Related": ["8-char hex Lore-id array -- informational links"]
  }
}
```

Only `intent` is required. Include only relevant trailers -- do not pad with empty values. `Lore-id` is auto-generated.

### When to Add Trailers

- You chose approach A over B: add `Rejected`
- A rule must hold going forward: add `Constraint`
- Future developers need an instruction: add `Directive`
- You are unsure: set `Confidence` to `"low"` or `"medium"`
- Change is hard to undo: set `Reversibility`
- You left something untested: add `Not-tested`

## Other Commands

```sh
lore context <path> --json     # Full context for a file/directory
lore why <file>:<line> --json  # Line-level blame with Lore context
lore search --text "q" --json  # Search across all lore
lore stale <path> --json       # Check for outdated decisions
lore trace <lore-id> --json    # Trace a decision chain
```

<!--LORE PROTOCOL END-->

<!--AGENTS-BASE:START v=20260920-->

## Rules

- **加新依赖**：装稳定版（如 `pnpm add pkg@latest`），不锁尝鲜版。
- **保持代码注释的克制**：不记决策和历史（历史走 Lore）。
- **对外 API**：保持命名/参数/类型风格，不破兼容；必须破时先提案确认。
- **写代码**：先跟项目既有风格，再跟语言官方指南。
- **改完代码**：立刻跑对应 lint/format/typecheck（如 `oxlint`/`oxfmt`、`tsc -b`、`cargo check`/`cargo fmt`、`go fmt`）。
- **写新逻辑**：先找项目内既有实现/工具包并复用；第二处重复即抽公共。
- **改导出符号**：先查全仓调用点，同步改完，不留 shim/别名。
- **删代码**：同步删调用方/导出/文档/测试，不留死代码。
- **修 bug**：先复现再修，修完确认复现消失。
- **做完**：跑覆盖改动路径的最小验证（测试/脚本/真机），不只编译过。
- **动架构/接口/跨模块**：先说问题+方案+影响范围，确认后再动手；局部小改进直接做。

## Design

- **接到需求**：先定可验证的成功标准，再动手。
- **有多种实现**：选最简单能过的，不预留扩展点。
- **想抽抽象**：第三次重复时再抽；两次只抽公共函数。
- **处理错误**：边界校验输入，内部 fail fast；不吞错、不给静默默认值。
- **改共享状态**：让非法状态不可表示；一次只改一处。
- **写测试**：测行为和边界，不测实现细节；抓不住 bug 的测试不写。
- **完成功能**：不留 TODO/占位/mock 充数；做不完就直说缺什么。
- **调异步接口**：loading/成功/失败/空数据四态齐全，失败给原因+重试，不白屏、不假死。

## Architecture

- **划模块**：按业务域拆，高内聚；跨模块只走公开接口，不钻内部实现。
- **定分层**：入口只做校验转发，业务放用例层，数据放仓储层；依赖自上而下，不反向、不成环。
- **收状态**：单一数据源，数据单向流；共享可变状态集中管，不散落各处。
- **藏实现**：对外暴露最小接口；改接口先加新再删旧，留迁移期。
- **隔配置**：环境/密钥/地址走配置或环境变量，不硬编码进逻辑。
- **留观测**：关键路径记日志/指标/错误，失败可定位，不黑盒跑。

## Security

- **碰密钥**：走环境变量或密钥管理，不进代码/日志/提交；疑泄漏即轮换。
- **接外部输入**：默认不可信；先校验类型/长度/范围，再使用。
- **拼 SQL/shell/HTML**：用参数化/转义，不手拼字符串。
- **做删除/覆盖/外发**：先确认影响面；不跑来源不明的脚本。

## Workflow

- **能查不问**：仓库/文档/工具能查到的直接查；真歧义一次问全。
- **改前确认**：破坏性/不可逆操作先说后果，确认后再做。
- **报结果**：说什么改了+怎么验证的；失败直说缺什么，不编结果。
- **一次一事**：一个改动只做一件事，不混无关重构。

<!--AGENTS-BASE:END v=20260920-->
