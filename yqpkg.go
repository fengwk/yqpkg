package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/beevik/etree"
)

// 为了解决文件导入后能顺序排列
const placeholder = "{counter}"

// 为了解决系统路径'/'无法适配的问题
const replacer = "|"

func yqpkgUsage() {
	fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [path...]\n", os.Args[0])
	flag.PrintDefaults()
}

func pkg(pt string) {
	absPath, err := filepath.Abs(pt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v", err)
		return
	}

	ext := filepath.Ext(absPath)
	if ext != ".epub" {
		fmt.Printf("can not support ext '%s'\n", ext)
		return
	}

	reader, err := zip.OpenReader(absPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v", err)
		return
	}
	defer reader.Close()

	zipName := filepath.Base(absPath)
	dirPath := zipName[:len(zipName)-len(ext)]
	tmpDir := filepath.Join(filepath.Dir(absPath), dirPath)
	if _, err := os.Stat(tmpDir); err != nil {
		err = os.Mkdir(tmpDir, os.ModePerm)
		if err != nil {
			fmt.Printf("can not create dir '%s', because %v\n", tmpDir, err)
			return
		}
	}

	zipFileMap := make(map[string]*zip.File)
	toc := etree.NewDocument()
	var tocPrefix string
	for _, zipFile := range reader.File {
		if zipFile.Name[len(zipFile.Name)-1] == '/' {
			continue
		}
		if filepath.Base(zipFile.Name) == "toc.ncx" {
			tocPrefix = filepath.Dir(zipFile.Name)
			if tocPrefix == "." {
				tocPrefix = ""
			}
			if tocPrefix != "" {
				tocPrefix += "/"
			}

			zfReader, err := zipFile.Open()
			if err != nil {
				fmt.Fprintf(os.Stderr, "open toc.ncx error, because %v\n", err)
				return
			}
			defer zfReader.Close()

			tocBytes, err := ioutil.ReadAll(zfReader)
			if err != nil {
				fmt.Fprintf(os.Stderr, "read toc.ncx error, because %v\n", err)
				return
			}

			err = toc.ReadFromBytes(tocBytes)
			if err != nil {
				fmt.Fprintf(os.Stderr, "parse toc.ncx error, because %v\n", err)
				return
			}
		} else {
			zipFileMap[zipFile.Name] = zipFile
		}
	}

	var destMap []*destEntry
	navs := toc.FindElements("ncx/navMap/navPoint")
	parse(navs, &destMap, tocPrefix, "")
	for zipFileName, zipFile := range zipFileMap {
		zfReader, err := zipFile.Open()
		if err != nil {
			fmt.Fprintf(os.Stderr, "open %s error, because %v\n", zipFileName, err)
			return
		}
		defer zfReader.Close()

		bytes, err := ioutil.ReadAll(zfReader)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read %s error, because %v\n", zipFileName, err)
			return
		}

		var destFilePath string
		if i, e := getEntry(destMap, zipFileName); e != nil {
			bytes = replaceTitle(bytes)
			prefix := fmt.Sprintf("%0"+strconv.Itoa(bitOf(len(destMap)))+"d", i)
			destPath := strings.Replace(e.dest, placeholder, prefix, 1)
			destFilePath = filepath.Join(dirPath, destPath)
		} else {
			destFilePath = filepath.Join(dirPath, zipFileName)
		}

		destFileDirPath := filepath.Dir(destFilePath)
		err = os.MkdirAll(destFileDirPath, os.ModePerm)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mkdir %s error, because %v\n", destFileDirPath, err)
			return
		}

		destFile, err := os.Create(destFilePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "create %s error, because %v\n", destFilePath, err)
			return
		}
		defer destFile.Close()

		_, err = destFile.Write(bytes)
		if err != nil {
			fmt.Fprintf(os.Stderr, "write %s error, because %v\n", destFilePath, err)
			return
		}
	}

	zipPkg(dirPath, dirPath+".zip")
	os.RemoveAll(tmpDir)
}

func bitOf(n int) int {
	c := 1
	n /= 10
	for n != 0 {
		c++
		n /= 10
	}
	return c
}

func zipPkg(src_dir, zip_file_name string) {
	// 预防：旧文件无法覆盖
	os.RemoveAll(zip_file_name)

	// 创建：zip文件
	zipfile, _ := os.Create(zip_file_name)
	defer zipfile.Close()

	// 打开：zip文件
	archive := zip.NewWriter(zipfile)
	defer archive.Close()

	// 遍历路径信息
	filepath.Walk(src_dir, func(path string, info os.FileInfo, _ error) error {

		// 如果是源路径，提前进行下一个遍历
		if path == src_dir {
			return nil
		}

		// 获取：文件头信息
		header, _ := zip.FileInfoHeader(info)
		header.Name = strings.TrimPrefix(path, src_dir+`\`)

		// 判断：文件是不是文件夹
		if info.IsDir() {
			header.Name += `/`
		} else {
			// 设置：zip的文件压缩算法
			header.Method = zip.Deflate
		}

		// 创建：压缩包头部信息
		writer, _ := archive.CreateHeader(header)
		if !info.IsDir() {
			file, _ := os.Open(path)
			defer file.Close()
			io.Copy(writer, file)
		}
		return nil
	})
}

func parse(navs []*etree.Element, destMap *[]*destEntry, tocPrefix, titlePrefix string) {
	for i, nav := range navs {
		title := nav.FindElement("navLabel/text").Text()
        title = strings.ReplaceAll(title, "/", replacer)
		src := nav.FindElement("content").SelectAttr("src").Value
		src = tocPrefix + strings.Split(src, "#")[0]
		destPath := filepath.Dir(src) + "/" + placeholder + "-" + title + ".html"
		if _, e := getEntry(*destMap, src); e == nil {
			*destMap = append(*destMap, &destEntry{src: src, dest: destPath})
		}
		parse(nav.FindElements("navPoint"), destMap, tocPrefix, titlePrefix+strconv.Itoa(i)+".")
	}
}

func getEntry(destMap []*destEntry, src string) (int, *destEntry) {
	for i, e := range destMap {
		if e.src == src {
			return i, e
		}
	}
	return -1, nil
}

type destEntry struct {
	src  string
	dest string
}

func replaceTitle(bytes []byte) []byte {
	//  text := string(bytes)
	//  lines := strings.Split(text, "\n")
	//  minTitle := 1
	//
	//  for _, line := range lines {
	//      if len(line) > 0 && line[0] == '#' {
	//          fmt.Println(line)
	//      }
	//
	//  }

	return bytes
}

func main() {
	flag.Usage = yqpkgUsage
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		flag.CommandLine.SetOutput(os.Stderr)
		yqpkgUsage()
	} else {
		for _, path := range args {
			pkg(path)
		}
	}
}
