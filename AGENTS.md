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

<!--AGENTS-BASE:START v=20261009-->

本模块是默认工程基线。具体实现遵循项目明确的架构、技术栈和业务契约，各条规则按其适用场景落实；项目差异不得绕过安全、授权和专项审批要求。

## Rules

- **明确目标**：实施前明确范围和可验证的完成条件；非简单变更先识别关键业务不变量、影响边界和主要失败路径，有实质取舍时说明选择依据。仅对影响方案且无法查明的歧义提问；必要假设明确说明，不自行扩展需求。
- **依据先行**：设计和实现前查阅相关项目契约、既有实现及文档；遇到不熟悉、不确定或版本敏感的技术问题，核对项目实际版本的官方文档或原始资料，不凭记忆推断 API 和行为。关键取舍说明依据，外部方案结合项目约束验证后采用。
- **按授权执行**：已有授权内的局部、可逆改动直接完成。破坏兼容、改变架构或公共契约、显著扩大范围，以及批量删除、不可逆数据变更、敏感信息外发或执行来源不明脚本，若未获明确授权，先说明影响、风险和迁移或恢复方式，再取得确认。
- **控制改动**：只修改完成目标所必需的内容，遵循项目约定和邻近代码风格；无约定时采用官方实践。只清理由本次改动造成的遗留，不夹带无关重构、全仓格式化或依赖升级。
- **维护契约**：修改接口、导出符号或删除代码前，查全仓调用、已知外部消费者及既有契约；同步更新受影响的调用、测试、文档和配置。默认保持兼容，仅在有明确迁移需求时保留兼容层。
- **验证行为**：修 bug 时先尽可能建立最小复现，再定位根因；修复后验证原问题及相关回归路径。代码改动后及时执行适用的 format/lint/typecheck，交付前完成覆盖实际行为、关键边界和失败路径的最小充分验证；测试不绑定内部实现，编译通过不代替功能验证。
- **如实交付**：交付路径不留占位实现、临时 mock 或假数据；说明实际修改、验证结果及未完成或未验证项。工具失败、无法复现或环境受限时明确说明依据和不确定性，不宣称成功。

## Engineering Design

- **整体简洁**：以系统整体的认知成本、耦合和变更影响衡量简洁，不以函数短小或抽象层少作为标准。先查找并复用已有能力；围绕业务概念、职责边界和不变量建立必要抽象，集中同一业务规则、隐藏实现细节，使变化局部化。抽象须有真实需求和明确收益，不必等到多次重复；避免无职责的转发层、碎片化拆分和假设性扩展点。
- **依赖克制**：新增依赖前确认现有能力不能满足需求；新增时选择当前稳定版，不主动引入预发布版本。
- **构建可复现**：使用项目声明的运行时和工具链，依赖变更同步更新项目锁文件；构建和验证不得依赖未记录的本机配置或手工修改的依赖。
- **职责清晰**：按业务域和职责组织模块，跨模块只依赖最小公开接口，不暴露内部实现；遵循项目分层，分离协议适配、鉴权校验、业务规则和数据访问，避免循环依赖。涉及权限或多租户时，在可信边界校验资源级授权与数据隔离。
- **状态唯一**：每类状态明确所有者和单一事实来源，收敛写入入口；派生数据优先计算，避免重复存储，尽量用类型和结构排除非法状态。
- **并发与竞态**：并发写入通过存储约束、原子操作或事务维护数据不变量，不依赖“先查后写”保证唯一性。请求有效性按所更新状态和业务语义判断：覆盖同一结果时防止旧响应覆盖新结果；独立并行、聚合和追加请求遵循各自的合并与顺序规则。
- **配置集中**：环境差异、服务地址和部署参数集中在配置中，避免业务逻辑散落环境判断或硬编码部署值。
- **错误可见**：内部违反不变量时及时暴露错误；不吞错、伪造成功或用默认值掩盖失败。关键路径保留足够且不含敏感信息的日志或错误上下文，使问题可定位。
- **超时与重试**：远程调用设置合理超时；需要重试时，限定次数并采用退避，仅重试可恢复的瞬时错误。重试有副作用的操作前，须保证幂等或具备去重机制；无法安全重复时不得自动重试。超时不代表操作未执行，避免多层叠加重试。
- **资源生命周期**：文件、连接、锁、定时器和事件订阅明确所有者与释放时机；成功、异常、取消或组件卸载时均须按生命周期清理，不遗留后台任务或监听器。
- **异步调用规则**：UI 数据读取必须处理 Loading（加载）、Success（成功）、Empty（空数据）、Error（失败）四态；提交类操作必须处理加载、成功和失败，存在空结果语义时处理 Empty。当前有效调用结束后清除 Loading；失败说明可理解的原因，可安全重试时提供入口，不以旧数据、空白界面或无限 Loading 掩盖失败。
- **UI 按钮状态**：按钮必须明确设计 Default（默认）、Hover（悬停）、Pressed（按下）、Disabled（禁用）、Loading（加载）五态。支持悬停的平台实现 Hover，异步操作按钮实现 Loading，同步按钮不制造加载过程。Disabled 不响应操作，Loading 阻止重复触发；优先复用组件已有状态实现，视觉遵循设计规范。
- **性能有据**：性能优化先测量并定位瓶颈，再验证收益；按实际数据规模检查查询次数、数据量和算法成本。无证据不引入缓存、并行化或复杂优化。
- **注释解释原因**：优先用命名、类型和结构表达意图，仅注释代码无法清楚表达的约束、取舍、边界或副作用；不复述实现或记录修改历史，随改动更新或删除失效注释。

## Security

- **保护凭据**：密钥、令牌和证书通过环境变量或密钥管理系统提供，不写入代码、日志、测试样例或提交记录；疑似泄漏按已泄漏处理并轮换。
- **校验输入**：用户、网络、文件、环境变量及外部系统的输入默认不可信；在系统边界按契约校验类型、格式、长度、范围和允许值。
- **防止注入**：SQL、shell 和 HTML 使用参数化 API、结构化参数或上下文正确的转义；不将不可信输入直接拼接为可执行语句或页面内容。

<!--AGENTS-BASE:END-->

<!--DESIGN.md:START-->

## Design System Governance

This project uses `DESIGN.md` (or another explicitly declared canonical design specification path) as the canonical design specification for UI implementation.

`AGENTS.md` defines the engineering workflow and approval boundaries; the canonical design specification defines the project's visual language and interaction guidance. Keep concrete design rules there and reference existing tokens/components instead of maintaining competing copies of their values.

### Before UI Work

Before creating or modifying any user-facing UI:

1. Read the canonical design specification if it exists.
2. Reuse existing theme tokens, shared components, and established UI patterns.
3. Do not introduce new visual conventions when an existing project pattern already solves the problem.
4. Identify the affected surface's information hierarchy, primary actions, states, and behavior across the supported viewport/input modes before implementation.

For normal feature work, inspect the affected surfaces; a full design-system audit is not required. If the task requires extracting, auditing, evolving the design system, or adapting an external design reference into project guidance, load the `design-md-extractor` skill.

### Design References and Validation

- External design analyses such as `awesome-design-md` are reference material, not an authoritative specification for this project or the source brand. Record the source and scope, distinguish observations from proposals, and map adopted semantic roles to project tokens/components. Do not copy a brand palette, marketing layout, or font choice into unrelated product surfaces by default.
- When a reference conflicts with the canonical specification, record the difference and propose the intended change separately. Selecting a reference does not grant permission to modify a Git-tracked canonical specification.
- Design guidance should cover the applicable visual direction, information density, semantic token roles, typography/layout hierarchy, component states, responsive behavior, themes, accessibility, and guardrails. Missing evidence belongs in the audit; do not invent values or behavior to fill a template.
- Verify changed UI through rendered output and interaction checks on representative supported sizes, input modes, and themes. Include affected loading/empty/error and button states, keyboard focus, and overflow/content extremes; report checks that cannot be performed. Compilation alone does not verify design compliance. Create or update a component preview only when it serves the requested design-documentation task; reuse existing preview tooling.

### DESIGN.md Authority

The canonical design specification is an evolvable project artifact, but it must not drift automatically during normal feature development.

If the canonical design specification is tracked by Git, treat it as approval-gated:

- You may inspect it.
- You may identify outdated, missing, or conflicting rules.
- You may generate a proposed patch or diff.
- You may update `DESIGN-AUDIT.md` and `DESIGN-DEBT.md`.

You MUST NOT modify a Git-tracked canonical design specification until the user explicitly approves that specific modification.

Explicit approval must clearly authorize modifying the canonical design specification, for example:

- "修改 DESIGN.md"
- "更新设计规范"
- "应用这个 DESIGN.md patch"

General approval such as "可以", "继续", "按这个方向做", approval of a UI proposal, feature implementation, refactor, or design direction does NOT authorize changing `DESIGN.md`.

Approval applies only to the specific proposed change and must not be reused for later modifications.

### Git Check

Before modifying the canonical design specification, determine its effective path first.

If the project-level canonical specification is `DESIGN.md`, the preferred check is:

```bash
git ls-files --error-unmatch DESIGN.md
```

If the project declares another canonical design specification path, run the same tracked-file check against that path.

If the check succeeds, explicit user approval is required before writing changes.

If the canonical design specification is not tracked, it may be created or iterated according to the `design-md-extractor` skill and available project evidence.

A failed Git command does not by itself establish that the file is untracked. If Git is unavailable, the directory is not a repository, or another error prevents verification, report the unknown status and leave an existing canonical specification unchanged without specific approval; complete the permitted audit/proposal instead.

### Design Evidence Modes

#### Extraction Mode

Use this mode when establishing or re-deriving the design system from the existing project and no approved canonical specification governs the inspected surface.

When project evidence conflicts, use this authority order:

1. Project theme / design tokens
2. Shared UI components
3. Layout primitives
4. Frequently reused feature components
5. Repeated page patterns
6. One-off local styles

Higher-authority evidence takes precedence over raw occurrence count.

Do not treat:

- one-off styles
- legacy pages
- third-party component defaults
- experimental pages

as canonical design rules unless explicitly established by the project.

#### Governance Mode

Use this mode when an approved canonical specification governs the inspected surface. Git tracking additionally controls the mutation approval gate; it does not determine whether current code can override an approved rule.

Authority order:

1. Approved canonical design specification
2. Explicitly approved design decisions
3. Project theme / design tokens
4. Shared UI components
5. Layout primitives
6. Frequently reused feature components
7. Repeated page patterns
8. One-off local styles

Existing implementation frequency does not override an approved design rule. Unapproved external references are proposal inputs, not an additional authority level.

If code and the canonical design specification conflict:

- Do not assume the code is correct.
- Do not assume the specification should be changed.
- Treat the approved specification as authoritative until the user approves otherwise; apply the independent tracked-file amendment gate before writing changes.
- Record implementation divergence in `DESIGN-DEBT.md` when appropriate.
- Propose specification changes separately from feature implementation.

### Lore and Design Approval

Lore provides decision context, constraints, rejected approaches, and directives. It does not grant permission to modify a Git-tracked canonical design specification.

For design-system changes:

- Query Lore as required by the Lore protocol.
- Treat Lore constraints/directives as inputs to the proposal.
- Do not treat Lore context as user approval.
- The DESIGN approval gate still applies independently.

### Normal Feature Development

During normal feature work:

- Treat the canonical design specification as read-only unless the user explicitly approves a design-system change.
- Prefer existing project theme tokens and semantic tokens.
- Prefer existing shared components over creating visually similar alternatives.
- Do not hardcode new colors, spacing, radii, shadows, or typography values without checking existing tokens first.
- Do not modify the canonical design specification merely to make a new implementation appear compliant.
- If implementation requirements conflict with the canonical design specification, report the conflict and propose a design-system change separately.
- When code diverges from an approved design rule, prefer recording the divergence as design debt over weakening the specification.

<!--DESIGN.md:END-->
