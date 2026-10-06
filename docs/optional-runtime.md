# 可选运行依赖与平台验收

基础发布产物是 Go 服务二进制和前端静态文件；Go/Node.js 只在构建环境需要。数据库、缓存、队列和对象存储继续使用现有 PostgreSQL、Redis、RabbitMQ、MinIO。媒体提取另需 FFmpeg 与同目录、同扩展名的 FFprobe。

| 层级 | 安装内容 | 不安装时 |
| --- | --- | --- |
| OCR | Tesseract 和配置中的语言包（默认 `chi_sim+eng`） | 关闭 OCR，已有转写可读 |
| 精确对齐 | 独立 Python 环境、平台推理库、固定 revision 的本地权重 | 普通 ASR 范围回放保留 |
| URL 导入 | yt-dlp；特殊来源按需配置 cookies/proxy | 设置 `upload.disable_url_import: true`，文件上传保留 |
| 监控 | 单独运行 Prometheus / Grafana | 应用日志和指标入口保留 |

开发浏览器工具不是生产依赖。基础部署包不包含 Python 环境或模型权重；对齐脚本、`alignment_model.py` 与 requirements 必须从同一 VidLens 源码版本安装到独立目录，服务配置使用 argv 数组，禁止把多参数写成 shell 字符串。

## 显式安装与离线运行

使用 Python 3.12 的独立虚拟环境。macOS arm64 使用 `mlx-audio==0.5.7`；Windows/Linux 的 CPU Adapter 使用 `qwen-asr==0.0.6`。两条路径由 `tools/requirements-alignment.txt` 的平台标记选择。CPU Adapter 的代码兼容性与真实推理验收是不同证据。

```sh
python -m venv .venv-alignment
# POSIX；Windows 使用 .venv-alignment\Scripts\python.exe
.venv-alignment/bin/python -m pip install -r tools/requirements-alignment.txt
```

管理员从[Qwen 官方模型说明](https://github.com/QwenLM/Qwen3-ASR#released-models-description-and-download)选择 PyTorch 权重，或从 [MLX Audio](https://github.com/Blaizzy/mlx-audio) 选择 MLX 权重。模型格式必须匹配执行后端。显式下载时固定完整 commit revision，不能使用会移动的 `main`。例如已安装 Hugging Face CLI 后，手动执行 `hf download MODEL_ID --revision COMMIT_SHA --local-dir MODEL_DIRECTORY`；这一步下载权重，普通状态读取和运行任务不下载。

```sh
# 从已下载的本地目录生成清单。完整扫描发生在安装/更新时。
python tools/transcript_align.py --backend mlx --model MODEL_DIRECTORY \
  --revision COMMIT_SHA --prepare-model-manifest
# Linux/Windows 用 --backend qwen；其余协议相同。
python tools/transcript_align.py --backend mlx --model MODEL_DIRECTORY --check
```

`--check` 校验模块可导入、固定 runtime 版本、模型目录和清单，不创建推理实例。普通能力读取检查 FFmpeg/FFprobe、OCR 版本/语言包、yt-dlp 和上述协议，最多缓存一分钟，单次总时限 20 秒；结果只含原因、版本和时间，不含私有路径或凭据。

清单包含每个权重/配置文件的 SHA-256、revision 和本地文件签名。任务只检查文件清单和签名，不反复读取大权重。替换、增加、删除文件后必须重新生成清单；同路径替换无法继续使用旧身份。清单绑定本地安装，复制到另一台机器后需重新准备。缓存键包含权重身份、revision、预处理/输出格式、后端、runtime 版本、语言、设备/dtype、音频哈希、文字及窗口；写入继续原子替换。模型清单在发布缓存和最终返回前再次验证。

手动 CapabilityProbe 才调用供应商，可能收费。成功或失败的模型/时间元数据绑定保存配置；未保存的草稿不能证明保存配置健康。当前配置与探测身份不同，旧健康记录失效。自检通过、小样本通过、真实音频推理通过不能互相替代。

对齐命令继续使用每个服务进程一个串行槽、默认 20 分钟时限与上下文取消。多实例各自有槽，需要部署者限制实例数和内存；没有集群并发上限的承诺。

## 平台记录要求

`.github/workflows/platform-check.yml` 只执行路径/取消/协议/缓存单测与基础构建，未执行的 CI 不标为通过，也不把这些检查计作真实 CPU 推理。

- Windows x64：实际启动、工具与空格路径、文件上传、FFmpeg、真实 CPU 音文对齐。
- Linux x64 CPU：独立安装、无 GPU 启动、真实音频对齐、取消/超时、峰值内存。
- macOS arm64：已有 MLX 路径、真实对齐及新旧缓存隔离。

本轮具体结果及尚未验收的平台记录在私有进度/交接文档中。基础应用不会因可选模型未安装而自动下载安装或拒绝所有功能。
