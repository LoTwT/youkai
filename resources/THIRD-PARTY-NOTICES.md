# Youkai 随包第三方声明

本文件随 Youkai 安装包分发（macOS 位于 `Youkai.app/Contents/Resources/`，Windows 位于应用 EXE 同目录），说明随包第三方组件的来源与许可。Youkai 本体以 MIT 许可发布。

组件事实与摘要的权威记录见仓库 [T3 规格](https://github.com/LoTwT/youkai/blob/main/docs/specs/t3-ocr-runtime-model-samples.md)（固定组合与制品静态核查）；本文件与之同步更新。

## ONNX Runtime

- 组件：ONNX Runtime CPU 运行库 1.29.0。macOS 为 `onnxruntime/libonnxruntime.1.29.0.dylib`；Windows 为 `onnxruntime/onnxruntime.dll` 与 `onnxruntime/onnxruntime_providers_shared.dll`。
- 来源：Microsoft 官方发布制品 `onnxruntime-osx-arm64-1.29.0.tgz` / `onnxruntime-win-x64-1.29.0.zip`（<https://github.com/microsoft/onnxruntime/releases/tag/v1.29.0>）。
- 许可：MIT。许可证原文与上游第三方声明随包放在 `onnxruntime/LICENSE` 与 `onnxruntime/ThirdPartyNotices.txt`。
- Windows 运行前提：VC++ v14 x64 运行库（`MSVCP140.dll`、`MSVCP140_1.dll`、`VCRUNTIME140.dll`、`VCRUNTIME140_1.dll`）按应用本地部署随包放在 EXE 同目录，取自 Visual Studio 的 `VC\Redist\MSVC` 再分发文件，按 Microsoft 的 Visual Studio 再分发条款分发。

## 识别模型

- 组件：`models/PP-OCRv6_rec_small.onnx`（PP-OCRv6 文字识别模型，small）。
- 来源：RapidAI/RapidOCR 的 ModelScope `v3.10.0` 资源（<https://www.modelscope.cn/models/RapidAI/RapidOCR/resolve/v3.10.0/onnx/PP-OCRv6/rec/PP-OCRv6_rec_small.onnx>），下载位置与摘要固定于 RapidOCR 仓库 v3.10.0 的 `default_models.yaml`。
- 许可：RapidOCR 仓库与该模型卡均声明 Apache-2.0。**注意：模型托管根目录的独立 `LICENSE` 文件尚未取得，模型原始来源、转换说明与最终适用声明须在正式交付前核对归档（项目交付清单已记录此项）；当前构建供开发与测试使用。**

## 构建时链接的库

以下组件不作为独立文件随包分发，编译链接进 Youkai 可执行文件：MyGo 原生 UI 框架（MIT，<https://github.com/egoist/mygo>）；Go 标准库与工具链（BSD 风格许可，<https://go.dev/LICENSE>）；Go 绑定 onnxruntime_go v1.36.0（MIT，<https://github.com/yalue/onnxruntime_go>）。
