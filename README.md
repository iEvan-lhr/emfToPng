# emfToPng 🎨

[![Go Reference](https://pkg.go.dev/badge/github.com/iEvan-lhr/emfToPng.svg)](https://pkg.go.dev/github.com/iEvan-lhr/emfToPng)
[![Go Version](https://img.shields.io/github/go-mod/go-version/iEvan-lhr/emfToPng)](https://golang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

一个轻量且高效的纯 Go 语言库，用于将 **EMF (Enhanced Metafile)** 格式的图片完美转换为 **PNG** 格式。支持 `[]byte` 和 `io.Reader`/`io.Writer` 流式操作，无 CGO 依赖，开箱即用。

---

## ✨ 特性

- **纯 Go 实现**：无需安装任何系统级依赖，不依赖 CGO，跨平台编译更简单。
- **简易的统一 API**：提供极简的 `Convert` 接口与 `ConvertEmfToPng` 快捷方法，可无缝处理字节数组和数据流。
- **高保真渲染**：支持 EMF 矢量图形及位图记录的绘制与解析，输出高质量的 PNG 图片。

---

## 📦 安装

在你的 Go 项目中，直接运行以下命令安装：

```bash
go get github.com/iEvan-lhr/emfToPng
```

---

## 🚀 快速上手

### 1. 基础转换（使用 `ConvertEmfToPng`）

下面是用于测试及基础转换的完整示例：

```go
package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/iEvan-lhr/emfToPng"
)

func TestConvertEmfToPng(t *testing.T) {
	// 1. 读取原始 EMF 文件字节流
	file, err2 := os.ReadFile("image1.emf")
	if err2 != nil {
		t.Error(err2)
	}

	// 2. 统一调用入口进行转换
	img, err2 := emf.ConvertEmfToPng(file)
	if err2 != nil {
		fmt.Printf("转换失败: %v\n", err2)
		return
	}
	filename := "image1.png"
	
	// 3. 将转换后的 PNG 写入本地文件
	err := os.WriteFile(filename, img, 0644)
	if err != nil {
		panic(err)
	}
	fmt.Println("🎉 转换成功！")
}
```

---

## 🛠️ 进阶用法 (统一调用入口 `Convert`)

除了方便的 `ConvertEmfToPng` 方法，我们还提供了高度通用的 `Convert` 函数。它能够自适应输入参数的类型（支持 `[]byte` 与 `io.Reader`）：

### 处理字节切片 `[]byte`

```go
package main

import (
	"os"
	"github.com/iEvan-lhr/emfToPng"
)

func main() {
	emfBytes, _ := os.ReadFile("input.emf")

	// 传入 []byte，返回的 any 类型可以安全断言为 []byte
	pngAny, err := emf.Convert(emfBytes)
	if err != nil {
		panic(err)
	}
	pngBytes := pngAny.([]byte)

	_ = os.WriteFile("output.png", pngBytes, 0644)
}
```

### 处理输入流 `io.Reader`

```go
package main

import (
	"io"
	"os"
	"github.com/iEvan-lhr/emfToPng"
)

func main() {
	emfFile, _ := os.Open("input.emf")
	defer emfFile.Close()

	// 传入 io.Reader，返回的 any 类型可以安全断言为 io.Reader
	pngAny, err := emf.Convert(emfFile)
	if err != nil {
		panic(err)
	}
	pngReader := pngAny.(io.Reader)

	outFile, _ := os.Create("output.png")
	defer outFile.Close()
	_, _ = io.Copy(outFile, pngReader)
}
```

---

## 🤝 致谢与致敬

本项目的代码一部分创意和源码来自于 [pzinovkin/emf](https://github.com/pzinovkin/emf)。

由于我们的**设计目的**与**未来愿景**有所不同，为了提供更简单直观的接口（例如提供统一的 `Convert` 自适应方法、更流畅的 API 调用链路以及轻量化的依赖管理），我们决定单独开设这个仓库并进行深度的重构与维护。在此向原作者的辛勤工作和开源精神致以诚挚的敬意！

---

## 📄 开源协议

本项目采用 [MIT License](LICENSE) 许可协议。
