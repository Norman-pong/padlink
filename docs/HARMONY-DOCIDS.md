# 鸿蒙端 devecocli 官方文档 ID 索引（口袋键鼠 / padlink）

> 用途：开发鸿蒙端时**现场查证 API 用法的唯一入口**。PRD §0.2 铁律 3：写 API 调用与记忆不符时必须现查，禁止凭记忆写参数（语音识别存在三处默认值陷阱）。
> 查询方法：
> - `devecocli docs search <内容关键词>`（按标题/正文关键词模糊匹配，**不能用短 ID 搜**，如搜 `faqs-arkui-907` 搜不到）
> - `devecocli docs read <完整文档ID>`（ID 即下表"完整路径"列，是全路径不是末段短 ID）

## 一、触摸与手势（FR-1 / FR-2 触摸板核心）

| 完整路径 | 关键事实 |
| --- | --- |
| `API参考/ArkUI_方舟UI框架/ArkTS组件/通用事件/基础输入事件/触摸事件/ts-universal-events-touch` | TouchObject.id = 手指唯一标识；**touches 按器件刷新率上报、changedTouches 按屏幕刷新率重采样**（低延迟光标必须消费 touches）；`getHistoricalPoints()` 取帧内被合并报点；TouchType.Cancel 触发场景（Home 返回、折叠切换） |
| `FAQ/UI框架/组件使用/多指touch中move事件下发时机问题/faqs-arkui-907` | move 事件由 vsync 按帧刷新下发，两帧间的多个报点只下发最新一个；**第二指按下会短暂丢失其 move 信息**（双指滚动起始段抖动的根因） |
| `FAQ/UI框架/组件使用/自定义组件拦截父组件的滚动/faqs-arkui-1397` | onTouch 通用事件与 enableScrollInteraction（滚动容器交互开关） |

## 二、网络（FR-0 发现配对 / FR-6 连接管理）

| 完整路径 | 关键事实 |
| --- | --- |
| `API参考/网络/Network_Kit_网络服务/ArkTS_API/ohos_net_socket_Socket连接_/js-apis-socket` | UDPSocket（constructUDPSocketInstance/bind/send）、TCPSocket、WebSocket；仅可发 string/ArrayBuffer（search 关键词用 "UDPSocket" 命中） |
| `API参考/网络/Network_Kit_网络服务/ArkTS_API/ohos_net_connection_网络连接管理_/js-apis-net-connection` | getAllNets / getNetCapabilitiesSync（筛 BEARER_WIFI）/ NetHandle.bindSocket（锁定出口网络）；**全系接口需要 `ohos.permission.GET_NETWORK_INFO` 声明，缺失报 201 Permission denied（模拟器实测踩坑）** |
| `FAQ/网络/网络_Network/如何限制UDPSocket通过特定网络发送广播/faqs-network-96` | 广播完整姿势：bind `0.0.0.0` → **bind 成功后** `setExtraOptions({broadcast:true})` → `netHandle.bindSocket(udpSocket)`；bindSocket 可多次调用绑定多个 Socket |
| `FAQ/网络/网络_Network/Socket通信时_如何根据使用场景正确转换数据类型/faqs-network-86` | ArrayBuffer ↔ string 转换规范（协议编解码要用） |
| `FAQ/网络/网络_Network/如何解决Socket_bind失败问题/faqs-network-115` | bind 错误码：2301013 Permission denied / 2301099 Address not available（联调排障） |

## 三、语音识别（FR-4 听写区）

| 完整路径 | 关键事实 |
| --- | --- |
| `API参考/Core_Speech_Kit_基础语音服务/ArkTS_API/speechRecognizer_语音识别/hms-ai-speechrecognizer` | CreateEngineParams（language 仅 'zh-CN'、online=1 离线）；StartParams.extraParams：**recognitionMode 默认 1=写流模式，实时录音必须显式传 0**、**maxAudioDuration 默认 20000（短语音上限 60000）**、vadEnd/vadBegin；SpeechRecognitionResult：**中间/最终结果靠同一回调的 isFinal 字段区分**；生命周期顺序 createEngine→setListener→startListening→finish/cancel→shutdown |
| `开发指南/Core_Speech_Kit_基础语音服务/语音识别/speechrecognizer-guide` | 权限声明完整 JSON：`ohos.permission.MICROPHONE` 必须带 `reason` + `usedScene{abilities:['EntryAbility'], when:'inuse'}`；运行时 requestPermissionsFromUser |
| `FAQ/机器学习/基础语音_Core_Speech/实时语音识别与音频文件识别的功能实现/faqs-core-speech-4` | 端侧离线执行、无网可用、数据不上传；短语音 ≤60s / 长语音 ≤8h |
| `FAQ/机器学习/基础语音_Core_Speech/语音识别自动停止或初始化失败问题解决方案/faqs-core-speech-12` | **vadEnd 默认 800ms**（可配 [500,10000]）——稍停顿即判停；建议显式配置 |
| `FAQ/机器学习/基础语音_Core_Speech/初始化语音识别引擎报错/faqs-core-speech-5` | 创建引擎常见报错与排查（errCode 1002200001 等） |
| `FAQ/机器学习/基础语音_Core_Speech/语音识别结果没有纠正如何解决/faqs-core-speech-9` | 热词机制：≤200 个、每个 2–20 字（v1.1 可选功能参考） |
| `API参考/Core_Speech_Kit_基础语音服务/ArkTS_API错误码/errorcode-corespeech` | 本模块错误码全集（1002200001 创建失败 / 1002200006 busy / 1002200012 无麦克风权限等） |

## 四、窗口、避让与沉浸式（§6.3 全屏触摸面）

| 完整路径 | 关键事实 |
| --- | --- |
| `API参考/ArkUI_方舟UI框架/ArkTS_API/窗口管理/ohos_window_窗口_/Enums/arkts-apis-window-e` | AvoidAreaType 全集：TYPE_SYSTEM(0 状态栏，**不含底部导航栏**)、TYPE_CUTOUT(1)、TYPE_SYSTEM_GESTURE(2，**官方注明所有设备均无此避让区域**)、TYPE_KEYBOARD(3)、TYPE_NAVIGATION_INDICATOR(4，**取底部手势条唯一途径**)、TYPE_FLOAT_NAVIGATION(5，API26+) |
| `FAQ/UI框架/窗口管理/如何解决获取导航栏高度失败的问题/faqs-arkui-824` | TYPE_SYSTEM 取不到底部高度的官方解释与正解 |
| `FAQ/UI框架/组件使用/如何获取状态栏和导航栏高度/faqs-arkui-202` | getWindowAvoidArea 返回值结构，**单位 px** |
| `FAQ/UI框架/UI界面/如何获取状态栏等避让区进行避让/faqs-arkui-1114` | setWindowLayoutFullScreen + avoidAreaChange 动态监听完整范式 |
| `FAQ/UI框架/窗口管理/如何实现沉浸式页面_包括沉浸式状态栏_沉浸式导航条/faqs-arkui-370` | 沉浸式两步法（setWindowLayoutFullScreen 或 expandSafeArea） |

## 五、工程脚手架与基建（M0 建工程 / HDS 合规）

| 完整路径 | 用途 |
| --- | --- |
| `FAQ/UI框架/组件使用/如何解决启动页背景和startWindowIcon属性设置为同一张图时出现的闪屏问题/faqs-arkui-1569` | 脚手架启动页避坑（startWindowIcon 与背景勿同图） |
| `API参考/ArkUI_方舟UI框架/ArkTS组件/通用属性/无障碍属性/ts-universal-attributes-accessibility` | 自建键盘/触控组件必须配无障碍属性（PRD §6.3、HDS 硬约束 11） |
| `开发指南/ArkTS_方舟编程语言/ArkTS并发/应用多线程开发实践/长时任务并发场景/长时任务开发指导_TaskPool/long-time-task-guide` | TaskPool 后台任务官方范式（v2 传感器采集场景；v1 发送循环参考） |
| `API参考/ArkTS_方舟编程语言/ArkTS_API/ohos_util_util工具函数_/js-apis-util` | `import { util } from '@kit.ArkTS'`；`util.generateRandomUUID(entropyCache?: boolean): string` = **加密安全**随机 RFC 4122 v4 UUID（API 9+，SystemCapability.Utils.Lang）——配对设备指纹来源（去连字符取 32 位 hex，见 `PairingCore.uuidToDeviceId`） |
| `FAQ/ArkTS语言/方舟编程语言_ArkTS/如何生成随机的uuid/faqs-arkts-14` | 随机 UUID 的官方推荐入口（指向 util.generateRandomUUID，勿用 Math.random 自拼） |

## 六、v2 预留能力（空中鼠标 / 息屏常采，v1 不使用）

| 完整路径 | 关键事实 |
| --- | --- |
| `API参考/硬件/Sensor_Service_Kit_传感器服务/ArkTS_API/ohos_sensor_传感器_/js-apis-sensor` | 陀螺仪数据 rad/s；`sensor.on(id, cb, {interval})` interval 单位**纳秒**；ACCELEROMETER/GYROSCOPE 权限 |
| `开发指南/Background_Tasks_Kit_后台任务开发服务/长时任务_ArkTS/continuous-task` | DATA_TRANSFER 类型长时任务；KEEP_BACKGROUND_RUNNING 权限；backgroundModes 声明 |
| `API参考/基础功能/Basic_Services_Kit_基础服务/ArkTS_API/设备管理/ohos_runningLock_RunningLock锁_/js-apis-runninglock` | RunningLock 防休眠；PROXIMITY_SCREEN_CONTROL 接近光锁 |
| `FAQ/基础功能/基础服务_Basics_Service/如何持有wakelock锁_防止系统休眠/faq-basics-service-kit-16` | wakelock 持有范式 |

## 七、已排除路线存档（避免重新踩坑）

| 完整路径 | 排除依据 |
| --- | --- |
| `API参考/硬件/Driver_Development_Kit_驱动开发服务/C_API/模块/HidDdk/capi-hidddk` | HID DDK 是**主机侧**能力（在本机内创建虚拟 HID / 访问外接 HID），不能让手机对外扮演 HID 外设——蓝牙外设路线排除的官方证据 |
| `FAQ/UI框架/组件使用/如何打开键鼠穿越功能开关/faqs-arkui-377` | "键鼠穿越/共享"为 HarmonyOS 设备间系统能力，不开放三方、不面向 Ubuntu |
| `API参考/网络/Connectivity_Kit_短距通信服务/ArkTS_API/ohos_bluetooth_ble_蓝牙ble模块_/js-apis-bluetooth-ble` | BLE GattServer 存在但无 HID Profile 官方支持——手写 HOGP 属无承诺灰色地带，不采用 |

---

## 八、ArkUI 状态管理 V2 与 @Builder 观察边界（调试面板 Realtime 冻结踩坑）

| 完整路径 | 关键事实 |
| --- | --- |
| `FAQ/UI框架/UI界面/Builder装饰器参数传递限制与使用方式介绍/faqs-arkui-1078` | **@Builder 按值传递参数时，参数（含状态变量）改变不会引起 @Builder 内 UI 刷新**；要刷新必须按引用传递（单参数 + 调用处直接传对象字面量）或按回调传递（UIUtils.makeBinding，API20+）。模拟器实测踩坑：DebugPanelPage statRow(label, value) 双参按值 → Realtime 区自打开起冻结，改单参对象字面量后恢复 |
| `开发指南/ArkUI_方舟UI框架/UI开发_ArkTS声明式开发范式/学习UI范式基本语法/组件扩展/Builder装饰器_自定义构建函数/arkts-builder` | 按引用传递仅「单参数且调用处直接传对象字面量」生效（≥2 参数或值/引用混传均不刷新）；@ObservedV2/@Trace 类实例**按值**传参仍具深度观测（改 @Trace 属性可刷新 Builder 内 UI）；@ComponentV2 下全局 Builder 传 @ObservedV2 实例必须按值（引用传递会被 ArkTS 语法拦截） |

---

维护约定：本表新增条目必须先在本机 `devecocli docs search` 核准完整路径再写入；发现路径失效（文档更新迁移）时，重新 search 定位新路径并更新本表。
