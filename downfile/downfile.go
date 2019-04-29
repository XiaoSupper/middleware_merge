package downfile

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/axgle/mahonia"
)

type Config struct {
	GoResultFilePath       string
	ResultFilenames        string
	GoResultMergeFilePath  string
	LuaParseResultFilePath string
	ScriptFilePath         string
	GoroutineNum           int
	BCCServerPort          []string
}
type JsonStruct struct {
}

var Configdata Config

func init() {
	JsonParse := NewJsonStruct()
	// configdata := Config{}
	//下面使用的是相对路径，config.json文件和main.go文件处于同一目录下
	JsonParse.Load("./config.json", &Configdata)
	fmt.Printf("GoroutineNum= %d", Configdata.GoroutineNum)
	fmt.Printf("GoResultFilePath= %d", Configdata.GoResultFilePath)
	fmt.Printf("GoResultMergeFilePath= %d", Configdata.GoResultMergeFilePath)

	fmt.Println("BCCServerPort= ")
	for _, one := range Configdata.BCCServerPort {
		fmt.Println(one)
	}

}

func NewJsonStruct() *JsonStruct {
	return &JsonStruct{}
}

func (jst *JsonStruct) Load(filename string, v interface{}) {
	//ReadFile函数会读取文件的全部内容，并将结果以[]byte类型返回
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return
	}
	//读取的数据为json格式，需要进行解码
	err = json.Unmarshal(data, v)
	if err != nil {
		return
	}
}

//go端结果文件保存路径从config.json中读取
var (
	Clustername = flag.String("clustername", Configdata.GoResultFilePath, "download clustername")
)

// 逐行读取文件内容
func ReadLines(fpath string) []string {
	fd, err := os.Open(fpath)
	if err != nil {
		panic(err)
	}
	defer fd.Close()

	var lines []string
	scanner := bufio.NewScanner(fd)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}

	return lines
}

func ReadAll(filePth string) ([]byte, error) {
	fmt.Println("进入downfile.ReadAll()函数，读取script文件\n")
	f, err := os.Open(filePth)
	if err != nil {
		return nil, err
	}
	return ioutil.ReadAll(f)
}

// 实现单个文件的下载
func Download(clustername string, filename string) string {
	fmt.Println("进入downfile.Download()函数，读取结果文件\n")
	nt := time.Now().Format("2019-04-03 15:04:05")
	fmt.Printf("[%s]\nTo download %s\n", nt, filename) //fileID文件路径

	durl := "http://:8081/downfiles/" + filename
	res, errRes := http.Get(durl)
	if errRes != nil {
		fmt.Printf("从BCC端下载文件出错： \n")
		panic(errRes)
	}
	fmt.Printf("clustername = %s\n", clustername)
	f, errf := os.Create(Configdata.GoResultFilePath + filename)
	if errf != nil {
		fmt.Printf("Go端创建文件出错： \n")
		panic(errf)
	}
	io.Copy(f, res.Body)
	return clustername + filename
}

func DownFileFromBCC() {
	//2.还是结果文件的URL列表，GO根据URL下载结果文件到GO所在地
	//这里认为是结果文件的URL列表

	fmt.Println("进入DownFileFromBCC()函数\n")
	bccfilePathlist := ReadLines(Configdata.ResultFilenames)
	if len(bccfilePathlist) == 0 {
		return
	}

	//并发下载多个结果文件
	ch := make(chan string)
	// 每个goroutine处理一个文件的下载
	for _, fileID := range bccfilePathlist {
		go func(fileID string) {
			fmt.Printf("fileID = %s\n", fileID)
			ch <- Download(*Clustername, fileID)
		}(fileID)
	}

	// 等待每个文件下载的完成，并检查超时
	timeout := time.After(900 * time.Second)
	for idx := 0; idx < len(bccfilePathlist); idx++ {
		select {
		case res := <-ch:
			nt := time.Now().Format("2019-04-04 11:11:11")
			fmt.Printf("[%s]Finish download %s\n", nt, res)
		case <-timeout:
			fmt.Println("Timeout...")
			break
		}
	}
	return
}

//用字符串切片将lua返回结果按行保存
func ReadLuaResultLine(filePth string, vlines []string) error {
	enc := mahonia.NewEncoder("gbk")
	f, err := os.Open(filePth)
	if err != nil {
		return nil
	}
	defer f.Close()

	bfRd := bufio.NewReader(f)
	for {

		line, err := bfRd.ReadBytes('\n')
		if strings.HasPrefix(string(line), enc.ConvertString("Parse:")) || strings.HasPrefix(string(line), enc.ConvertString("Path:")) || strings.HasPrefix(string(line), enc.ConvertString("SubPath:")) || strings.HasPrefix(string(line), enc.ConvertString("Query:")) {
			continue
		} else {
			vlines = append(vlines, string(line))
		}
		//hookfn(line) //放在错误处理前面，即使发生错误，也会处理已经读取到的数据。
		if err != nil { //遇到任何错误立即返回，并忽略 EOF 错误信息
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
	return nil
}
