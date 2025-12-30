package utils

import (
	"archive/zip"
	"fmt"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shogo82148/androidbinary/apk"
)

// BuildApkFilename 生成服务端落盘的 APK 文件名（带 .apk 扩展名）
func BuildApkFilename(packageName, version string, originalName string) string {
	pkg := SanitizeFileToken(packageName)
	ver := SanitizeFileToken(version)
	ts := time.Now().Format("20060102_150405")
	short := strings.ReplaceAll(uuid.New().String(), "-", "")[:6]

	ext := ".apk"
	if originalName != "" {
		if e := strings.ToLower(filepath.Ext(originalName)); e == ".apk" {
			ext = e
		}
	}

	return fmt.Sprintf("%s_%s_%s_%s%s", pkg, ver, ts, short, ext)
}

// ValidateApkFile 校验文件是否为有效的 APK 文件
// APK 文件本质是 ZIP 文件，且必须包含 AndroidManifest.xml
func ValidateApkFile(filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("打开文件失败: %w", err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("获取文件信息失败: %w", err)
	}
	if fi.Size() == 0 {
		return fmt.Errorf("文件为空")
	}

	// 检查文件头是否为 ZIP 格式（ZIP 文件以 "PK" 开头）
	header := make([]byte, 4)
	if _, err := f.ReadAt(header, 0); err != nil {
		return fmt.Errorf("读取文件头失败: %w", err)
	}
	if header[0] != 'P' || header[1] != 'K' {
		return fmt.Errorf("不是有效的 ZIP 文件（APK 必须是 ZIP 格式）")
	}

	// 尝试作为 ZIP 文件打开
	zr, err := zip.NewReader(f, fi.Size())
	if err != nil {
		return fmt.Errorf("不是有效的 ZIP 文件: %w", err)
	}

	// 检查是否包含 AndroidManifest.xml
	hasManifest := false
	for _, file := range zr.File {
		if strings.Contains(file.Name, "AndroidManifest.xml") {
			hasManifest = true
			break
		}
	}
	if !hasManifest {
		return fmt.Errorf("不是有效的 APK 文件（缺少 AndroidManifest.xml）")
	}

	return nil
}

// ApkInfo 从 APK 文件解析出的信息
type ApkInfo struct {
	PackageName string // 包名
	VersionName string // 版本名称（例如 "1.0.0"）
	VersionCode int64  // 版本代码（整数，例如 1）
	IconPath    string // 提取的图标保存路径（如果成功提取）
}

// ParseApkFile 解析 APK 文件，提取包名、版本、图标等信息
// apkPath: APK 文件路径
// iconSaveDir: 图标保存目录（如果为空则不在该目录保存图标）
// iconSaveDir 如果提供，会将图标保存为 PNG 格式到该目录，文件名格式：{package_name}_icon.png
func ParseApkFile(apkPath, iconSaveDir string) (*ApkInfo, error) {
	pkg, err := apk.OpenFile(apkPath)
	if err != nil {
		return nil, fmt.Errorf("打开 APK 文件失败: %w", err)
	}
	defer pkg.Close()

	info := &ApkInfo{}

	// 提取包名
	info.PackageName = pkg.PackageName()

	// 提取版本信息
	manifest := pkg.Manifest()
	info.VersionName = manifest.VersionName.MustString()
	info.VersionCode = int64(manifest.VersionCode.MustInt32())

	// 提取图标并保存
	if iconSaveDir != "" {
		iconImg, err := pkg.Icon(nil)
		if err != nil {
			// androidbinary库可能不支持某些APK的图标格式（如WebP、AVIF等），这是库的限制
			log.Printf("提取APK图标失败 (包名: %s): %v (注: 某些APK的图标格式不被androidbinary库支持，这是已知限制)", info.PackageName, err)
		} else if iconImg == nil {
			log.Printf("APK图标为nil (包名: %s): APK中未找到可用的图标资源", info.PackageName)
		} else {
			// 生成图标文件名
			pkgNameSafe := SanitizeFileToken(info.PackageName)
			if pkgNameSafe == "" {
				pkgNameSafe = "unknown"
			}
			iconFilename := fmt.Sprintf("%s_icon.png", pkgNameSafe)
			iconPath := filepath.Join(iconSaveDir, iconFilename)

			// 确保目录存在
			if err := os.MkdirAll(iconSaveDir, 0755); err != nil {
				return nil, fmt.Errorf("创建图标目录失败: %w", err)
			}

			// 保存图标为 PNG
			outFile, err := os.Create(iconPath)
			if err != nil {
				return nil, fmt.Errorf("创建图标文件失败: %w", err)
			}
			defer outFile.Close()

			if err := png.Encode(outFile, iconImg); err != nil {
				_ = os.Remove(iconPath) // 清理失败的文件
				return nil, fmt.Errorf("保存图标失败: %w", err)
			}

			info.IconPath = iconPath
			log.Printf("成功提取并保存APK图标 (包名: %s, 路径: %s)", info.PackageName, iconPath)
		}
		// 图标提取失败不影响其他信息，继续返回包名和版本
	}

	return info, nil
}

