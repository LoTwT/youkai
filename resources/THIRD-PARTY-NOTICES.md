# Youkai 随包第三方声明

本文件随 Youkai 安装包分发（macOS 位于 `Youkai.app/Contents/Resources/`，Windows 位于应用 EXE 同目录），说明随包第三方组件的来源与许可。Youkai 本体以 MIT 许可发布。

组件事实与摘要的权威记录见仓库 [T3 规格](https://github.com/LoTwT/youkai/blob/main/docs/specs/t3-ocr-runtime-model-samples.md)（固定组合与制品静态核查）；本文件与之同步更新。

## ONNX Runtime

- 组件：ONNX Runtime CPU 运行库 1.32.0。macOS 为 `onnxruntime/libonnxruntime.1.32.0.dylib`；Windows 为 `onnxruntime/onnxruntime.dll` 与 `onnxruntime/onnxruntime_providers_shared.dll`。
- 来源：Microsoft 官方发布制品 `onnxruntime-osx-arm64-1.32.0.tgz` / `onnxruntime-win-x64-1.32.0.zip`（<https://github.com/microsoft/onnxruntime/releases/tag/v1.32.0>）。
- 许可：MIT。许可证原文与上游第三方声明随包放在 `onnxruntime/LICENSE` 与 `onnxruntime/ThirdPartyNotices.txt`。
- Windows 运行前提：VC++ v14 x64 运行库（`MSVCP140.dll`、`MSVCP140_1.dll`、`VCRUNTIME140.dll`、`VCRUNTIME140_1.dll`）按应用本地部署随包放在 EXE 同目录，取自 Visual Studio 的 `VC\Redist\MSVC` 再分发文件，按 Microsoft 的 Visual Studio 再分发条款分发。

## 识别模型

- 组件：`models/PP-OCRv6_rec_small.onnx`（PP-OCRv6 文字识别模型，small）。
- 来源与归属见 [RapidOCR 模型许可与归属声明](https://github.com/RapidAI/RapidOCRDocs/blob/256173e56c46ccd173ef89d1b256900f9baed80b/docs/model_licenses.md)。RapidOCR 将 PaddleOCR 的 `PP-OCRv6_small_rec` 转换为 ONNX 并通过 ModelScope `v3.10.0` 分发。上游模型权重版权归百度及／或相应 PaddleOCR 权利人所有。Youkai 随包分发 RapidOCR 提供的原文件。
- 模型按 Apache-2.0 分发，许可证原文见随包的 [`licenses/apache-2.0.txt`](licenses/apache-2.0.txt)。[PaddlePaddle 官方 ONNX 模型卡](https://huggingface.co/PaddlePaddle/PP-OCRv6_small_rec_onnx)也标明该许可。

## 构建时链接的库

以下组件不作为独立文件随包分发，编译链接进 Youkai 可执行文件：MyGo 原生 UI 框架（MIT，<https://github.com/egoist/mygo>）；Go 标准库与工具链（BSD 风格许可，<https://go.dev/LICENSE>）；Go 绑定 onnxruntime_go v1.38.0（MIT，<https://github.com/yalue/onnxruntime_go>）。

## 第五版原生界面组件

- Lobe MyGo：`github.com/lobehub/lobe-mygo v0.0.0-20261110162706-5e5fea5ef9e2`，提交 `5e5fea5ef9e225f79564770ef18f6d293a2e5d4a`。本应用链接其 theme、action 与 lucide 包，采用 MIT 许可，原文见随包 `licenses/lobe-mygo-LICENSE.txt`。来源：<https://github.com/lobehub/lobe-mygo/tree/5e5fea5ef9e225f79564770ef18f6d293a2e5d4a>。
- Lucide 图标：固定于上述 Lobe 提交，采用 ISC 许可；其中派生自 Feather 的图标另含 Cole Bemis 的 MIT 声明。上游完整声明见随包 `licenses/lucide-LICENSE.txt`。未使用的二维码组件没有链接进应用。
