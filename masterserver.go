package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"user_client_go_server/server_mastergo/downfile"

	"github.com/axgle/mahonia"
)

type Respon struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
}

func main() {
	server := http.Server{
		Addr: "localhost:8080",
	}
	//Addr: ":8080",
	http.HandleFunc("/getQuery", getQuery)       //Get
	http.HandleFunc("/upload", uploadScriptFile) //POST  Multipart
	http.HandleFunc("/", rootHandle)
	server.ListenAndServe()
}

func rootHandle(w http.ResponseWriter, r *http.Request) {
	fmt.Println("404 Not Found")
}
func getQuery(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	query, qError := r.Form["query"]
	returntype, oError := r.Form["returntype"] //也可将需要服务器进行的操作需求作为表单键值对传入
	//returntype:buffer & 结果文件列表

	var result Respon
	if !qError || !oError {
		result.Code = "401"
		result.Msg = "Get请求失败"
	} else {
		urlparam := "http://:8002/parse?query=" + query[0]
		// respLua, err := http.Post(urlparam, "application/x-www-form-urlencoded", strings.NewReader("mobile=xxxxxxxxxx&isRemberPwd=1"))
		respLua, err := http.Get(urlparam)
		if err != nil {
			fmt.Println(err)
			return
		}
		defer respLua.Body.Close()

		bodyLua, err := ioutil.ReadAll(respLua.Body)
		if err != nil {
			fmt.Println(err)
			return
		}

		fmt.Println(string(bodyLua))

		//将lua解析的结果存入utf8文件
		saveluautf8 := downfile.Configdata.LuaParseResultFilePath + "luaParseResult.utf8.html"
		fileutf8, _ := os.Create(saveluautf8)
		defer fileutf8.Close()

		s := string(bodyLua)
		fileutf8.WriteString((string(bodyLua)))
		enc := mahonia.NewEncoder("gbk")
		//将lua解析的结果存入gbk文件
		saveluagbk := downfile.Configdata.LuaParseResultFilePath + "luaParseResult.gbk.html"
		filegbk, _ := os.Create(saveluagbk)
		filegbk.WriteString(enc.ConvertString(s))

		//读取lua解析结果文件，处理后发送给BCC服务
		var lines []string
		downfile.ReadLuaResultLine(saveluagbk, lines)

		var scriptToBCC []string
		scriptToBCC = setScriptToBCC(lines)

		var strQueryUtf8 string
		for _, line := range scriptToBCC {
			strQueryUtf8 = strQueryUtf8 + line
		}

		//并发向BCC发送检索请求
		var goresultMergeFile string
		var bRetALL bytes.Buffer
		goresultMergeFile = SendRequest2BCC(strQueryUtf8, bRetALL)

		startFileSystem()
		if returntype[0] == "nofile" {
			bRetALL.WriteTo(w)
		} else {
			w.Write([]byte(goresultMergeFile))
		}
	}

	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Fatal(err)
	}
}

func uploadScriptFile(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	returntype, oError := r.Form["returntype"]
	fmt.Printf("returntype = %s\n", returntype)
	var result Respon
	if !oError {
		result.Code = "401"
		result.Msg = "POST请求失败"
	}
	if r.Method == "POST" {

		filePath := upload(w, r)
		fmt.Printf("filePath = %s\n", filePath)
		script, _ := downfile.ReadAll(filePath)
		scriptstr := string(script[:])
		fmt.Printf("切片长度len(script) = %d\n", len(script))
		fmt.Printf("scriptstr = %s\n", scriptstr)

		//向BCC发送检索请求
		//并发向BCC发送检索请求

		var goresultMergeFile string
		var bRetALL bytes.Buffer
		goresultMergeFile = SendRequest2BCC(scriptstr, bRetALL)
		startFileSystem()
		if returntype[0] == "nofile" {
			bRetALL.WriteTo(w)
		} else {
			w.Write([]byte(goresultMergeFile))

		}
		// BCC服务可能返回：1.结果存放的地址(位于BCC服务所在地)、结果列表 2.结果文件
	}

}

// 处理/upload 逻辑
func upload(w http.ResponseWriter, r *http.Request) string {
	fmt.Println("进入upload()函数\n")
	fmt.Println("method:", r.Method) //获取请求的方法
	if r.Method == "GET" {
		t, _ := template.ParseFiles("upload.gtpl") //加载模板，返回一个模板对象和错误
		log.Println(t.Execute(w, nil))
		return ""
		// t.Execute(w, token)
	} else {
		r.ParseMultipartForm(32 << 20)
		file, handler, err := r.FormFile("uploadfile")
		if err != nil {
			fmt.Println(err)
		}
		defer file.Close()

		fmt.Fprintf(w, "%v", handler.Header)
		f, err := os.OpenFile(downfile.Configdata.ScriptFilePath+handler.Filename, os.O_WRONLY|os.O_CREATE, 0666) // 此处假设当前目录下已存在test目录

		if err != nil {
			fmt.Println(err)
		}
		defer f.Close()
		io.Copy(f, file)
		return downfile.Configdata.ScriptFilePath + handler.Filename
	}

}

func SendRequest2BCC(strQueryUtf8 string, bRetALL bytes.Buffer) string {
	fmt.Println("进入SendRequest2BCC()函数\n")
	client := &http.Client{}
	// var bRetALL bytes.Buffer
	var ifsave bool = false
	fd, _ := os.OpenFile(downfile.Configdata.ResultFilenames, os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND, 0644)
	sep := "\n"
	strQueryUtf8Lines := strings.Split(strQueryUtf8, sep)
	for _, line := range strQueryUtf8Lines {
		if strings.HasPrefix(line, "Save") {
			ifsave = true
			fmt.Printf("改行匹配成功: %s\n", line)
			firstIndex := strings.Index(line, "\"")
			suffix := []byte(line)[firstIndex+1 : len(line)]
			linesuffix := string(suffix)
			secondIndex := strings.Index(linesuffix, "\"")
			preffix := []byte(linesuffix)[0:secondIndex]
			filename := string(preffix)
			fmt.Printf("保存文件名 %s\n", filename)
			buf := []byte(filename)
			fd.Write(buf)

		}
	}
	fd.Close()
	enc := mahonia.NewEncoder("gbk")
	strQueryGBK := enc.ConvertString(strQueryUtf8)

	//goroutine数量根据配置文件中BCC服务数量确定
	goroutineNum := downfile.Configdata.GoroutineNum
	urlList := downfile.Configdata.BCCServerPort
	fmt.Println("BCCServerPort= ")
	for _, one := range downfile.Configdata.BCCServerPort {
		fmt.Printf("BCCServerPort = %s", one)
	}
	fmt.Printf("BCC服务个数len(urlList)：%d", len(urlList))

	var wg sync.WaitGroup
	wg.Add(goroutineNum)
	for n := 0; n < goroutineNum; n++ {
		go func(n int) {
			defer wg.Done()
			fmt.Printf("strQueryGBK = %s", strQueryGBK)
			reqBCC, err := http.NewRequest("POST", urlList[n],
				strings.NewReader(strQueryGBK))
			if err != nil {
				log.Println(err)
				return
			}
			reqBCC.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			respBCC, err := client.Do(reqBCC)
			defer respBCC.Body.Close()

			bodyBCC, err := ioutil.ReadAll(respBCC.Body)
			if err != nil {
				fmt.Println(err)
				return
			}

			fmt.Printf("BCC返回结果bodyBCC：%s", string(bodyBCC))
			if !ifsave {
				bRetALL.Write(bodyBCC)
			}

		}(n)
	}
	wg.Wait()
	//BCC服务可能返回：1.结果存放的地址(位于BCC服务所在地)、结果列表 2.结果文件
	//GO对结果文件合并
	//将GO端保存的结果文件列表作为响应传给User
	var goMergedFilePath string
	if ifsave {
		downfile.DownFileFromBCC()
		//合并结果文件
		var rootPath = downfile.Configdata.GoResultFilePath
		goMergedFilePath = merge(rootPath)
	}
	return goMergedFilePath
}
func setScriptToBCC(linesFromLua []string) []string {

	var mark = []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15", "16", "17", "18", "19", "20", "21", "22", "23", "34", "25", "26", "27", "28", "29", "30"}
	var n string = "\n"
	var markIndex int = 0
	var script []string
	var array1 []string
	var array2 []string

	//GetAS
	for index := 0; index < len(linesFromLua); index++ {
		sep := "\t"
		array1 = strings.Split(linesFromLua[index], sep)
		getAS := GetASLine(array1)
		stras := "Handle" + mark[markIndex] + getAS + n
		script = append(script, stras)
		markIndex++
	}

	for index := 0; index < len(linesFromLua)-1; index++ {
		sep := "\t"
		if (index + 1) < len(linesFromLua) {
			array2 = strings.Split(linesFromLua[index+1], sep)
			//JoinAS
			joinAS := "Handle" + mark[markIndex] + "=JoinAS(Handle" + mark[index] + ",Handle" + mark[index+1] + ",\"" + array2[6] + "\")" + n
			script = append(script, joinAS)

			// context
			context := "Handle" + mark[markIndex] + "Context(Handle" + mark[markIndex] + ",4)" + n
			script = append(script, context)
			//Del
			del := "Del(\"result" + mark[index] + ".txt\")" + n
			script = append(script, del)
			//Save
			save := "Save(Handle" + mark[markIndex] + ", \"result" + mark[index] + ".txt\")" + n
			script = append(script, save)
			//Output
			output := "Output(Handle" + mark[markIndex] + ", 3)" + n
			script = append(script, output)
			markIndex++

		}
	}
	return script
}
func GetASLine(a1 []string) string {
	var getAS string
	if a1[4] == "nil" {
		a1[4] = ""
		if a1[5] == "nil" {
			a1[5] = ""
			getAS = "=GetAS(\"" + a1[0] + "\",\"" + a1[1] + "\",\"" + a1[4] + "\",\"" + a1[5] + "\")"
		} else {
			getAS = "=GetAS(\"" + a1[0] + "\",\"" + a1[1] + "\",\"" + a1[4] + "\",\"(" + a1[5] + ")\")"
		}
	} else {
		if a1[5] == "nil" {
			a1[5] = ""
			getAS = "=GetAS(\"" + a1[0] + "\",\"" + a1[1] + "\",\"(" + a1[4] + ")\",\"" + a1[5] + "\")"
		} else {
			getAS = "=GetAS(\"" + a1[0] + "\",\"" + a1[1] + "\",\"(" + a1[4] + ")\",\"(" + a1[5] + ")\")"
		}
	}
	return getAS
}

func merge(rootPath string) string {
	fmt.Println("进入merge()函数\n")
	fix := string(time.Now().Unix())
	outFileName := downfile.Configdata.GoResultMergeFilePath + "merge_result" + fix + ".txt"
	outFile, openErr := os.OpenFile(outFileName, os.O_CREATE|os.O_WRONLY, 0600)
	if openErr != nil {
		fmt.Printf("Can not open file %s", outFileName)
	}
	bWriter := bufio.NewWriter(outFile)
	filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		fmt.Println("Processing:", path)
		//这里是文件过滤器，表示我仅仅处理txt文件
		if strings.HasSuffix(path, ".txt") {
			fp, fpOpenErr := os.Open(path)
			if fpOpenErr != nil {
				fmt.Printf("Can not open file %v", fpOpenErr)
				return fpOpenErr
			}
			bReader := bufio.NewReader(fp)
			for {
				buffer := make([]byte, 1024)
				readCount, readErr := bReader.Read(buffer)
				if readErr == io.EOF {
					break
				} else {
					bWriter.Write(buffer[:readCount])
				}
			}
		}
		return err
	})
	bWriter.Flush()
	return outFileName
}

func startFileSystem() {
	fmt.Printf("启动Go端文件系统\n")
	mux := http.NewServeMux()
	//http://:8081/downfiles/xxx.txt
	downfilepath := strings.TrimRight(downfile.Configdata.GoResultMergeFilePath, "/")
	files := http.FileServer(http.Dir(downfilepath))

	mux.Handle("/downfiles/", http.StripPrefix("/downfiles/", files))
	server := http.Server{
		Addr:    "localhost:8082",
		Handler: mux,
	}
	mux.HandleFunc("/", rootHandle)
	server.ListenAndServe()
}
