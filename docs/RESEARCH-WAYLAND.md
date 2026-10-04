# PadLink Wayland 集成点技术方案（Ubuntu 26.04 / GNOME Wayland）

> 调研方式：仅查证 upstream 一手来源（内核/libinput/systemd/mutter/gnome-session/wl-clipboard/ydotool/wtype 官方仓库与官方文档）
> 对应 PRD 章节：§2（架构）、§4.5（语音文本注入）、§7（daemon 规格）
> 结论速览：**六项全部可行，其中四项有需在实现中规避的坑；无一项动摇 PRD 架构**

可信度标记：★★★★★ = upstream 官方文档/源码/规则文件原文；★★★★☆ = 官方发行说明/官方包仓库；★★★☆☆ = 官方文档中对"可能发生"的一般性警告（未在 GNOME 上有专项量化数据）。

---

## 1. uinput 高分辨率滚轮（REL_WHEEL_HI_RES）

### 结论：**可行**（一个行为坑：注册了 HI_RES bit 就必须真的发 HI_RES 事件）

### 依据

| 事实 | 来源 | 可信度 |
| --- | --- | --- |
| `REL_WHEEL_HI_RES`/`REL_HWHEEL_HI_RES` 于 **Linux 5.0** 引入（commit `52ea8996` "Input: add REL_WHEEL_HI_RES and REL_HWHEEL_HI_RES"，2018-12 合入，2019-03 随 5.0 发布） | https://github.com/torvalds/linux/commit/52ea899637c746984d657b508da6e3f2686adfca | ★★★★★ |
| 语义：**累积值 120 = 移动一个 detent（格）**；不支持高分辨率的设备该值恒为 120 的倍数；高分辨率设备可为 120 的分数；"若滚轮支持高分辨率滚动，HI_RES 将**与 REL_WHEEL 同时**发出"；REL_WHEEL 可能是基于 HI_RES 的近似值；优先使用 HI_RES | 内核官方文档 https://www.kernel.org/doc/html/latest/input/event-codes.html | ★★★★★ |
| libinput 对内核 ≥5.0 的高分辨率滚轮支持：`LIBINPUT_EVENT_POINTER_SCROLL_WHEEL`（1.19+）携带 v120 值（`libinput_event_pointer_get_scroll_value_v120`），与 Windows 滚轮 API 的 120 单位约定一致；内核不支持 HI_RES 时 libinput 为普通滚轮点击**模拟**高分辨率事件 | libinput 官方文档 https://wayland.freedesktop.org/libinput/doc/latest/wheel-api.html | ★★★★★ |
| libinput 源码行为（1.31.3）：`REL_WHEEL_HI_RES` 累积进 `wheel.hi_res` → 产生 `SCROLL_WHEEL`（v120，过滚动加速 filter）；`REL_WHEEL` 累积进 `wheel.lo_res` → **只产生已废弃的 `LIBINPUT_EVENT_POINTER_AXIS`** 事件（`evdev_notify_axis_legacy_wheel`，无 v120）。两条路径独立累计、互不抵消 | libinput 源码 `src/evdev-fallback.c`（经 sources.debian.org 取 1.31.3-1）https://sources.debian.org/src/libinput/ | ★★★★★ |
| mutter（GNOME Shell 合成器）**显式忽略** `LIBINPUT_EVENT_POINTER_AXIS`（"This event must be ignored in favor of the SCROLL_* events"），只消费 `SCROLL_WHEEL` 并用 v120 值产生离散+平滑滚动 | mutter 源码 `src/backends/native/meta-seat-impl.c` https://gitlab.gnome.org/GNOME/mutter/-/blob/main/src/backends/native/meta-seat-impl.c | ★★★★★ |
| **坑的官方记录**：libinput 官方排错页明确点名 **uinput 虚拟设备**——"固件、内核驱动或用户态软件（含经 uinput 创建虚拟设备的软件）"若声明了 HI_RES 能力位却只发 `REL_WHEEL`，libinput 打一次性警告并回退为模拟模式；可用 quirk `AttrEventCode=-REL_WHEEL_HI_RES;-REL_HWHEEL_HI_RES;` 关掉错误能力位 | https://wayland.freedesktop.org/libinput/doc/latest/incorrectly-enabled-hires.html | ★★★★★ |

### 推导：与 REL_WHEEL 同时注册是否有坑？

- 对 **GNOME（mutter）**：无坑。mutter 只看 SCROLL_WHEEL（HI_RES 路径），legacy AXIS 被丢弃。注册两者、只发 HI_RES、或按内核惯例两者都发（REL_WHEEL=取整 detent），GNOME 表现一致，不会双滚。
- 对**绕过合成器直接读 evdev 的程序**（个别游戏、barrier/input-leap 类工具）：若 daemon 同时发 REL_WHEEL 和 REL_WHEEL_HI_RES，这类程序可能双计。**建议 daemon 只发 HI_RES**（注册 bit 与事件一致，同时避开 libinput 排错页场景与直读程序双计）；REL_WHEEL bit 可按 PRD §7.2 保留注册（对 libinput 无影响，因为 lo_res 计数为 0 不会产生事件），但更干净的做法是干脆不注册 REL_WHEEL。
- Ubuntu 26.04 内核为 7.0（见第 5 节），远高于 5.0 门槛，无版本问题；libinput ≥1.19（v120 API）自 2021 年起所有 Ubuntu LTS 均满足。

### 对 daemon 实现的约束建议

1. uinput 设备注册 `REL_WHEEL_HI_RES`（水平滚如需再加 `REL_HWHEEL_HI_RES`）。
2. SCROLL 事件**只发 HI_RES**：协议字段 `SCROLL(dy_hi_res)`（PRD §5.2）语义直接映射为"v120 单位"（120 = 1 格）；手机端双指滑动累积映射到 HI_RES 值（如每像素 8–15 单位），由 libinput/mutter 合成平滑滚动与自然滚动设置。
3. 禁止"注册 HI_RES 却发 REL_WHEEL"的组合（触发 libinput 警告回退路径）。

### 给 PRD 的修正建议

- §7.2 uinput 模块职责中"REL_WHEEL + REL_WHEEL_HI_RES"建议改为"REL_WHEEL_HI_RES（唯一发出的事件流；可选注册 REL_WHEEL 以最大兼容，但事件只发 HI_RES）"，并在 §4.2 表格注明"120 units = 1 detent"换算关系。

---

## 2. udev uaccess 授予 /dev/uinput

### 结论：**可行且必要**——上游与 Ubuntu 均不给 /dev/uinput 任何默认授权，PRD 自带 udev 规则的方案是对的；uaccess 绑定 logind 活跃座位会话

### 依据

| 事实 | 来源 | 可信度 |
| --- | --- | --- |
| 上游 systemd `70-uaccess.rules` 给 drm/card*、renderD*、kvm、rfkill、udmabuf 等打 `TAG+="uaccess"`，**没有 uinput 条目** | systemd 仓库 `rules.d/70-uaccess.rules.in`（main 分支原文）https://github.com/systemd/systemd/blob/main/rules.d/70-uaccess.rules.in | ★★★★★ |
| 上游 `50-udev-default.rules` 也无 uinput 条目；`SUBSYSTEM=="input", GROUP="input"` **不覆盖** uinput——uinput 注册在 `misc` 子系统（该文件对 misc 只处理 sgx 设备）→ stock 系统上 /dev/uinput 为 root:root 0600 | systemd 仓库 `rules.d/50-udev-default.rules.in` https://github.com/systemd/systemd/blob/main/rules.d/50-udev-default.rules.in | ★★★★★ |
| Ubuntu 26.04（resolute）udev 包完整规则文件列表核实：同样**没有任何 uinput 规则**（80-debian-compat 等亦无） | https://packages.ubuntu.com/resolute/amd64/udev/filelist | ★★★★★ |
| uaccess 官方定义：sd-login(3) "Tag uaccess: When set, **access to this device is tied to an active seat**. As the session on the seat becomes active or inactive, access to the device is updated accordingly."（即依赖 systemd-logind 的活跃本地会话；ACL 按 UID 写入设备节点） | https://www.freedesktop.org/software/systemd/man/latest/sd-login.html | ★★★★★ |
| canonical 规则写法（与 KDE Connect 2025 年新规则完全一致）：`SUBSYSTEM=="misc", KERNEL=="uinput", TAG+="uaccess", OPTIONS+="static_node=uinput"` | AntiMicroX `other/60-antimicrox-uinput.rules`、KDE Connect `plugins/digitizer/40-kdeconnect-uinput.rules`、KDE plasma-bigscreen 等 | ★★★★★ |

### systemd user service 场景可行性

- uaccess 的 ACL 以**用户 UID**写入 `/dev/uinput` 的 POSIX ACL（`getfacl /dev/uinput` 可见 `user:<uid>:rw-`），因此同 UID 的 `systemd --user` 实例（user@.service 及其下单元）天然可读写——**user service 场景可行**。
- 时序要求：ACL 在图形会话激活时由 logind 施加。daemon 若挂在 `default.target`（登录即启动）会在 ACL 尚未写入的窗口内打开失败。**应挂 `graphical-session.target`**（见第 3 节）。
- 已知发行版差异：无实质差异——Debian/Ubuntu/Fedora/Arch 均直接用上游 systemd 规则（uinput 一律无默认授权）；差异只在各发行版个别软件包自带的补充规则。

### 对 daemon 实现的约束建议

1. 规则文件 `dist/udev/60-padlink.rules` 内容：
   ```
   SUBSYSTEM=="misc", KERNEL=="uinput", TAG+="uaccess", OPTIONS+="static_node=uinput"
   ```
   `OPTIONS+="static_node=uinput"` 确保开机即创建设备节点并应用 tag（与 KDE Connect/AntiMicroX 同款）；不加 MODE/GROUP（uaccess 足够，避免引入组依赖）。
2. 安装文档必须包含生效步骤：`sudo udevadm control --reload && sudo udevadm trigger /dev/uinput`（或重启），验证命令 `getfacl /dev/uinput | grep $(id -u)`。
3. daemon 启动时若 `/dev/uinput` 打开失败（EACCES），报错文案指向"udev 规则未安装/未生效 + getfacl 自查"，不要静默重试。
4. 遥控/SSH 场景明确不支持：无活跃本地座位会话则 uaccess 不授权（与 PRD 仅桌面使用场景一致）。

### 给 PRD 的修正建议

- §7.1 建议把"udev 规则授予 uaccess"细化为上述 canonical 规则 + reload/trigger 步骤；§10 提交门增加一条联调前置检查（getfacl 验证）。

---

## 3. wl-clipboard 在 systemd user service 中运行

### 结论：**可行，有 4 个可规避的坑**（环境变量时序、wl-copy 后台驻留进程生命周期、wl-paste 默认追加换行、恢复时机竞态）；GNOME 内置剪贴板管理器提供了系统性兜底

### 依据

| 事实 | 来源 | 可信度 |
| --- | --- | --- |
| `wl-copy` 默认 **fork 到后台并驻留**以服务 paste 请求（`-f/--foreground` 才留在前台）——剪贴板内容的所有权在 wl-copy 进程存活期间维持 | wl-clipboard 官方 man page（仓库 `data/wl-clipboard.1`）https://github.com/bugaevc/wl-clipboard | ★★★★★ |
| `wl-paste` 默认在 stdout 输出**末尾追加换行符**；`-n/--no-newline` 关闭（非文本类型与 --watch 模式自动关闭） | 同上 | ★★★★★ |
| 若合成器不支持 wlroots data-control 协议，wl-clipboard 依靠"弹出一个微型透明 surface 抢焦点"的 hack 访问剪贴板；个别情况下合成器不给焦点 → wl-paste 表现为挂起 | 同上（BUGS 节） | ★★★★☆（官方警告；GNOME 上通常正常，hang 为个案） |
| **mutter 未实现 `zwlr_data_control_device_manager_v1`**（源码全文检索无）→ GNOME 上 wl-clipboard 走上述弹窗路径；mutter 实现 wp/gtk-primary-selection（仅 primary 选区） | GNOME/mutter 仓库代码检索 | ★★★★★ |
| **mutter 内置剪贴板管理器**：每次剪贴板 owner 变化即缓存内容（text/plain、text/plain;charset=utf-8 上限 4MB；image/* 上限 200MB）；当 owner 消失（客户端死亡）导致选区清空时，mutter 用内存副本**自动接管**——剪贴板内容不会因 wl-copy 进程死亡而丢失 | mutter 源码 `src/core/meta-clipboard-manager.c` https://gitlab.gnome.org/GNOME/mutter/-/blob/main/src/core/meta-clipboard-manager.c | ★★★★★ |
| GNOME 会话启动时 gnome-session 把 `WAYLAND_DISPLAY`、`DISPLAY` 等通过 `org.freedesktop.systemd1` SetEnvironment（等价 `systemctl --user import-environment`）写入 user manager 环境；切换会话时会从 unsetlist 中清除旧值防泄漏 | gnome-session 源码 `gnome-session/gsm-util.c` https://gitlab.gnome.org/GNOME/gnome-session/-/blob/main/gnome-session/gsm-util.c | ★★★★★ |
| systemd 官方模式：依赖图形会话的 user service 应 `PartOf=graphical-session.target` 并 `WantedBy=graphical-session.target`（官方示例即 GNOME 场景）；graphical-session.target 由 gnome-session.target 以 BindsTo 启停 | systemd.special(7) https://www.freedesktop.org/software/systemd/man/latest/systemd.special.html | ★★★★★ |
| `wl-clipboard` 无法同时提供/恢复多 MIME 类型（官方已知限制） | man page BUGS 节 + https://github.com/bugaevc/wl-clipboard/issues/71 | ★★★★★ |
| GTK3 应用复制文本带 CRLF（\r\n）；wl-clipboard 不做转换（备份-恢复按字节原样往返则不受影响） | man page BUGS 节 | ★★★★★ |

### 四个坑与规避

1. **环境变量时序**：`XDG_RUNTIME_DIR` 对 user manager 恒有（logind/PAM 注入）；`WAYLAND_DISPLAY` 只在图形会话启动后才被 gnome-session 导入。daemon 挂 `default.target` 可能在变量就绪前启动。
   **规避**：unit 写 `PartOf=graphical-session.target` + `[Install] WantedBy=graphical-session.target`（并 `After=graphical-session.target`）；daemon 启动时校验 `WAYLAND_DISPLAY` 缺失则给出明确错误。不要用 environment.d（它管不了运行时动态变量）。
2. **wl-copy 后台进程被 cgroup 连坐**：fork 出的 wl-copy 属于 daemon 所在 user service 的 cgroup，daemon stop/restart 时被 systemd 默认 KillMode 杀掉 → 当时剪贴板看似被清空。
   **规避（GNOME 下已被 mutter 剪贴板管理器兜底：内容由内存副本继续提供，用户无感）**；若追求完全干净，恢复动作可改用 `systemd-run --user --scope wl-copy …` 把驻留进程放到独立 scope。验收口径见下面 PRD 修正。
3. **wl-paste 默认追加换行**：备份若用裸 `wl-paste`，恢复后内容尾部多出 `\n`，违反 §11.4"事后剪贴板无变化"。
   **规避**：备份一律 `wl-paste --no-newline`（并建议先 `wl-paste --list-types` 记录原 MIME）；恢复 `wl-copy --type <原MIME>` 原样写回（不要用 `wl-copy -n`，它会裁掉合法的尾部换行）。注入文本用 `wl-copy --type 'text/plain;charset=utf-8'`（规避 man BUGS 所述文本类型推断失灵问题）。
4. **恢复时机竞态**：Ctrl+V 后应用异步向合成器请求数据；若 300ms 内恢复已替换数据源，慢应用可能粘到旧内容。另注意：mutter 在每次 owner 变化时立即自读一份（缓存），因此 `wl-copy -o/--paste-once` 的"服务一次即退出"在 GNOME 上不等价于"应用已粘贴"（第一次请求往往是 mutter 自己）。
   **规避**：维持固定延迟但做成可配置（默认 300ms、可调 500ms+）；不要依赖 `--paste-once` 作为粘贴完成信号；不要在恢复前提前退出注入用 wl-copy。

补充：GNOME 无 wlr-data-control ⇒ 每次 wl-paste 理论上都有一次隐形弹窗往返，实测通常无感；建议 daemon 对 wl-paste/wl-copy 均设 2–3s 超时，超时按"剪贴板功能降级"处理（与缺 wl-clipboard 同一降级路径）。

### 给 PRD 的修正建议

- §4.5 注入流程细化为：`wl-paste --list-types`（记 MIME）→ `wl-paste --no-newline`（备份，仅驻内存）→ `wl-copy --type text/plain;charset=utf-8` → 注入 Ctrl+V → 可配延迟（默认 300ms）→ `wl-copy --type <原MIME>` 恢复。
- §7.1 unit 挂载目标从 user service 泛述改为明确 `graphical-session.target` 模式。
- §11.4 验收口径补充："剪贴板内容不变"指**内容字节**不变；owner 在 daemon 停止后可能变为 mutter 内置管理器（内存源），属正常。
- §13 风险表增加两行：wl-copy 驻留进程随服务重启被杀（缓解：mutter 内存接管 / systemd-run scope）；wl-paste 弹窗 hack 个别情况挂起（缓解：超时+降级）。

---

## 4. GNOME Wayland 对 uinput 相对设备的指针加速

### 结论：**有坑（双重加速）**：GNOME 默认对相对指针设备施加 adaptive 加速，PRD 手机端加速曲线会被叠加；无 env/udev 手段改 profile，唯一正解是主机侧设 flat profile 并据此标定

### 依据

| 事实 | 来源 | 可信度 |
| --- | --- | --- |
| libinput 对**所有相对运动设备**（鼠标/触摸板/trackpoint）施加设备特定指针加速，默认 profile 为 **adaptive**（慢速 1:1、快速线性增益；系数范围 0.3–3.5）；flat 为恒定系数 1:1；配置 API（profile/speed）**由合成器调用** | libinput 官方文档 https://wayland.freedesktop.org/libinput/doc/latest/pointer-acceleration.html | ★★★★★ |
| libinput **不读取任何环境变量**；quirks 体系（udev 属性/quirks 文件）中**没有**设置默认加速 profile 的条目（quirks 仅 Model/Attr 硬件修正类）——即"LIBINPUT_ACCELERATION_PROFILE"这类环境变量/udev 属性**不存在** | libinput 官方 quirks 文档与 pointer-acceleration 文档 | ★★★★★ |
| mutter 对鼠标类设备应用**全局** gsettings：`org.gnome.desktop.peripherals.mouse` 的 `speed` 与 `accel-profile`（default→libinput 默认即 adaptive；可选 flat；GNOME 4x 还有 custom）；per-device GSettings 仅覆盖 tablet/touchscreen（`lookup_device_settings` 只为这两类建 per-device 路径）→ PadLink 虚拟鼠标与物理鼠标共享同一全局设置 | mutter 源码 `src/backends/meta-input-settings.c`、`src/backends/native/meta-input-settings-native.c` https://gitlab.gnome.org/GNOME/mutter/-/blob/main/src/backends/meta-input-settings.c | ★★★★★ |
| 设备识别基于能力位与 vendor/product：自定义 vendor/product 的 uinput 设备不会命中任何 libinput quirk → 行为即"普通鼠标默认 adaptive"；udev 的 ID_INPUT_* 由能力位（60-input-id.rules）判定，与设备名无关 | libinput/udev 上游行为（50/60/70 规则文件 + libinput 文档） | ★★★★★ |

### 对 daemon 实现的约束建议

1. **默认假设主机为 flat**：PRD §4.2 的加速曲线（慢速 1:1、快速 2.5x、灵敏度 1.2）应以 `gsettings set org.gnome.desktop.peripherals.mouse accel-profile 'flat'` 为基线标定；README/首次运行向导给出该命令（一条命令，用户成本极低）。
2. daemon 可在启动时读 `gsettings get org.gnome.desktop.peripherals.mouse accel-profile`（user bus 在 user service 中可用，`busctl --user call` 即可）——非 flat 时打警告（甚至把手机端曲线自动切到"直通模式"），避免双重加速造成的手感不可复现。
3. **不要**尝试自动改用户的 accel-profile（影响其物理鼠标），只提示。
4. 调试面板显示当前检测到的 host accel-profile，便于 §8 延迟/手感验收时归因。

### 给 PRD 的修正建议

- §4.2 增加"主机指针加速假设"一段：GNOME 默认 adaptive 会与手机曲线叠加；产品以 flat 为推荐配置并在 daemon 检测提示；曲线默认值按 flat 标定。
- §8 验收指标的手感部分注明前提"主机 accel-profile=flat"。

---

## 5. Ubuntu 26.04 LTS 事实核查

### 结论：**PRD 平台假设全部成立**，且"仅 Wayland"在 26.04 上是强制现实（GNOME 桌面已无 X11 会话）

| 项 | 核实结果 | 来源 | 可信度 |
| --- | --- | --- | --- |
| 代号 | **Resolute Raccoon**，2026-04 发布 | Ubuntu 官网发布周期表（canonical/ubuntu.com 仓库 `templates/about/release_cycles/releases-table.html`） | ★★★★★ |
| 发布与支持 | 2026-04-23 发布；LTS 支持至 2031-04（Ubuntu Pro ESM 至 2036） | https://documentation.ubuntu.com/release-notes/26.04/ | ★★★★★ |
| 内核 | **Linux 7.0**（GA 从 24.04 的 6.8 跳到 7.0；HWE 从 6.17 到 7.0）→ REL_WHEEL_HI_RES（5.0+）无忧 | 同上（"Changes since 25.10"与"Summary for LTS users"两页交叉确认） | ★★★★★ |
| 桌面 | **GNOME 50**（从 25.10 的 GNOME 49 升级）；Ubuntu Desktop 默认 GNOME | 同上 | ★★★★★ |
| Wayland 默认 | **Ubuntu Desktop 会话自 25.10 起仅运行 Wayland 后端**（GNOME Shell 已不能以 X.org 会话运行；X11 应用走 XWayland）——PRD"明确不做 X11 适配"在 26.04 不是取舍而是事实 | 同上 | ★★★★★ |
| wl-clipboard | resolute/universe 有 **wl-clipboard 2.2.1-2build1**，`sudo apt install wl-clipboard` 直装（需启用 universe，Ubuntu Desktop 默认启用） | https://packages.ubuntu.com/resolute/wl-clipboard | ★★★★★ |
| evtest | resolute/universe 有 **evtest 1:1.36-1**，直装 | https://packages.ubuntu.com/resolute/evtest | ★★★★★ |

### 给 PRD 的修正建议

- §1.1/§1.3 可补充一句："26.04 的 GNOME 桌面仅提供 Wayland 会话，X11 检测分支仅作为防御性警告存在"——把"明确不做 X11"从产品决策升级为客观约束，验收口径更硬。
- §4.5 的安装提示命令 `sudo apt install wl-clipboard` 确认可用。

---

## 6. 番外：Ctrl+V 注入的替代方案对比

### 结论：**无可行替代，剪贴板 + Ctrl+V 是 GNOME Wayland 上中文文本注入的正解**（两个候选均被一手源码排除）

| 方案 | 核查结果 | 来源 | 可信度 |
| --- | --- | --- | --- |
| **ydotool type** | **不支持非 ASCII**：`type` 命令用硬编码 `ascii2keycode_map[128]` 表（char→keycode+Shift），非 ASCII 字符直接 `return` 丢弃——中文完全不可用 | ydotool 源码 `Client/tool_type.c` https://github.com/ReimuNotMoe/ydotool/blob/master/Client/tool_type.c | ★★★★★ |
| **wtype** | 依赖 `zwp_virtual_keyboard_manager_v1`（virtual-keyboard-unstable-v1）协议，**mutter 未实现该协议**（代码检索无 zwp_virtual_keyboard）→ GNOME 上不可用（Sway/wlroots 系可用；其 README 的 unicode 示例仅在支持该协议的合成器上成立） | wtype 源码 `protocol/virtual-keyboard-unstable-v1.xml` + mutter 代码检索 https://github.com/atx/wtype | ★★★★★ |
| mutter input-method-v2 | mutter 内部有 ClutterInputMethod 抽象，但**未对外暴露 zwp_input_method_v2** Wayland 接口（源码树无 meta-wayland-input-method）——"伪装成输入法提交文本"路线在 GNOME 不可行，且与 fcitx/ibus 冲突 | mutter 源码树结构 | ★★★★★ |
| xdg-desktop-portal RemoteDesktop / libei | 存在但需 D-Bus + 权限对话框，且键盘注入接口是 keycode/keysym 级，**同样无任意文本 commit API** | mutter/FDO 相关接口 | ★★★★☆ |

**对比建议**：维持 PRD 方案。附带两个可选优化：
1. daemon 直接 `exec` wl-clipboard 二进制（Go 无 cgo 约束不受影响，wl-clipboard 是独立程序）——无需引入 Wayland 客户端库。
2. v2 若要摆脱对 wl-clipboard 二进制的依赖，正确方向是实现 wl_seat data-device 客户端（即 wl-copy 做的事），而不是换注入通道；收益仅是少一个外部依赖，优先级低。

### 给 PRD 的修正建议

- 无架构改动；可在 §2.1 要点里补一句"已核查 ydotool（仅 ASCII）与 wtype（GNOME 无 virtual-keyboard 协议）均不可用，剪贴板路线是 GNOME 上唯一工程可行解"，避免后续被反复质疑。

---

## 7. 汇总

| # | 核查项 | 结论 | 最大风险/坑 |
| --- | --- | --- | --- |
| 1 | REL_WHEEL_HI_RES | 可行 | 注册 HI_RES bit 就必须发 HI_RES 事件，否则 libinput 告警回退 |
| 2 | udev uaccess | 可行且必要 | 上游/Ubuntu 无默认授权；需 canonical 规则 + reload/trigger；ACL 依赖活跃图形会话时序 |
| 3 | wl-clipboard | 可行（4 个坑） | wl-paste 默认尾加换行；wl-copy 驻留进程随服务重启被杀（mutter 兜底）；恢复时机竞态 |
| 4 | 指针加速 | 有坑（可控） | GNOME adaptive 与手机曲线双重加速；无 env/udev 手段，需主机 flat profile 基线 |
| 5 | Ubuntu 26.04 | 全部假设成立 | 无（universe 需启用，Desktop 默认有） |
| 6 | Ctrl+V 替代 | 无替代 | ydotool 仅 ASCII、wtype 无 GNOME 协议支持（源码实锤） |

**架构结论：六项均不动摇 PRD 架构。** 需要落地的 PRD 增量：graphical-session.target 挂载、canonical udev 规则与生效文档、wl-clipboard 命令参数细化（--no-newline/--type）、flat 加速基线假设、验收口径补充。
