package install

import (
	"fmt"
	"github.com/choerodon/c7n/pkg/config"
	"github.com/choerodon/c7n/pkg/slaver"
	"github.com/vinkdong/gox/log"
	"github.com/vinkdong/gox/random"
	"golang.org/x/crypto/ssh/terminal"
	"gopkg.in/yaml.v2"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"math/rand"
	"os"
	"regexp"
	"syscall"
	"time"
)

var Ctx Context

const (
	PvType        = "pv"
	PvcType       = "pvc"
	CRDType       = "crd"
	ReleaseTYPE   = "helm"
	SucceedStatus = "succeed"
	FailedStatus  = "failed"
	// if have after process while wait
	CreatedStatus      = "created"
	staticLogName      = "c7n-logs"
	staticLogKey       = "logs"
	staticInstalledKey = "installed"
	randomLength       = 4
)

type Context struct {
	Client        kubernetes.Interface
	Namespace     string
	CommonLabels  map[string]string
	SlaverAddress string
	Slaver        *slaver.Slaver
	UserConfig    *config.Config
	BackendTasks  []*BackendTask
}

type BackendTask struct {
	Name    string
	Success bool
}

// i want use log but it make ...
type News struct {
	Name      string
	Namespace string
	RefName   string
	Type      string
	Status    string
	Reason    string
	Date      time.Time
	Resource  config.Resource
	Values    []ChartValue
	PreValue  PreValueList
}

type NewsResourceList struct {
	News []News `yaml:"logs"`
}

func (ctx *Context) AddBackendTask(task *BackendTask) bool {
	for _, v := range ctx.BackendTasks {
		if v.Name == task.Name {
			return false
		}
	}
	ctx.BackendTasks = append(ctx.BackendTasks, task)
	return true
}

func (ctx *Context) HasBackendTask() bool {
	for _, v := range ctx.BackendTasks {
		if v.Success == false {
			return true
		}
	}
	return false
}

func (ctx *Context) SaveNews(news *News) error {
	data := ctx.GetOrCreateConfigMapData(staticLogName, staticLogKey)
	nr := &NewsResourceList{}
	yaml.Unmarshal([]byte(data), nr)
	news.Date = time.Now()
	if news.RefName == "" {
		news.RefName = news.Name
	}
	nr.News = append(nr.News, *news)
	newData, err := yaml.Marshal(nr)
	if err != nil {
		log.Error(err)
		return err
	}
	ctx.saveConfigMapData(string(newData[:]), staticLogName, staticLogKey)

	if news.Status == SucceedStatus || news.Status == CreatedStatus {
		ctx.SaveSucceed(news)
	}
	return nil
}

func (ctx *Context) UpdateCreated(name, namespace string) error {

	nr := ctx.getSucceedData()
	isUpdate := false
	for k, v := range nr.News {
		if v.Name == name && v.Namespace == namespace && v.Status == CreatedStatus {
			v.Status = SucceedStatus
			nr.News[k] = v
			isUpdate = true
		}
	}
	if !isUpdate {
		log.Infof("nothing update with app %s in ns: %s", name, namespace)
	}
	newData, err := yaml.Marshal(nr)
	if err != nil {
		log.Error(err)
		return err
	}
	ctx.saveConfigMapData(string(newData[:]), staticLogName, staticInstalledKey)
	return nil
}

func (ctx *Context) SaveSucceed(news *News) error {

	news.Date = time.Now()
	nr := ctx.getSucceedData()
	nr.News = append(nr.News, *news)
	newData, err := yaml.Marshal(nr)
	if err != nil {
		log.Error(err)
		return err
	}
	ctx.saveConfigMapData(string(newData[:]), staticLogName, staticInstalledKey)
	return nil
}

func (ctx *Context) GetSucceed(name string, resourceType string) *News {
	nr := ctx.getSucceedData()
	for _, v := range nr.News {
		if v.Name == name && v.Type == resourceType {
			// todo: make sure gc effort
			p := v
			return &p
		}
	}
	return nil
}

func (ctx *Context) DeleteSucceed(name, namespace, resourceType string) error {
	nr := ctx.getSucceedData()
	index := -1
	for k, v := range nr.News {
		if v.Name == name && v.Namespace == namespace && v.Type == resourceType {
			index = k
		}
	}

	if index == -1 {
		log.Infof("nothing delete with app %s in ns: %s", name, namespace)
		return nil
	}
	nr.News = append(nr.News[:index], nr.News[index+1:]...)
	newData, err := yaml.Marshal(nr)
	if err != nil {
		log.Error(err)
		return err
	}
	ctx.saveConfigMapData(string(newData[:]), staticLogName, staticInstalledKey)
	// todo save delete to log
	return nil
}

func (ctx *Context) getSucceedData() *NewsResourceList {
	data := ctx.GetOrCreateConfigMapData(staticLogName, staticInstalledKey)
	nr := &NewsResourceList{}
	yaml.Unmarshal([]byte(data), nr)
	return nr
}

func (ctx *Context) GetOrCreateConfigMapData(cmName, cmKey string) string {
	if ctx.Client == nil {
		log.Error("Get k8s client failed")
		os.Exit(127)
	}
	cm, err := ctx.Client.CoreV1().ConfigMaps(ctx.Namespace).Get(cmName, meta_v1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("creating logs to cluster")
			cm = ctx.createNewsData()
		}
	}
	return cm.Data[cmKey]
}

func (ctx *Context) createNewsData() *v1.ConfigMap {

	data := make(map[string]string)
	data[staticLogKey] = ""
	cm := &v1.ConfigMap{
		TypeMeta: meta_v1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: meta_v1.ObjectMeta{
			Name:   staticLogName,
			Labels: ctx.CommonLabels,
		},
		Data: data,
	}
	configMap, err := ctx.Client.CoreV1().ConfigMaps(ctx.Namespace).Create(cm)
	if err != nil {
		log.Error(err)
		os.Exit(122)
	}
	return configMap
}

func (ctx *Context) saveConfigMapData(data, cmName, cmKey string) *v1.ConfigMap {

	cm, err := ctx.Client.CoreV1().ConfigMaps(ctx.Namespace).Get(cmName, meta_v1.GetOptions{})
	cm.Data[cmKey] = data
	configMap, err := ctx.Client.CoreV1().ConfigMaps(ctx.Namespace).Update(cm)
	if err != nil {
		log.Error(err)
		os.Exit(122)
	}
	return configMap
}

func IsNotFound(err error) bool {
	errorStatus, ok := err.(*errors.StatusError)
	if ok && errorStatus.Status().Code == 404 {
		return true
	}
	return false
}

type Exclude struct {
	Start int
	End   int
}

func RandomInt(min, max int, exclude ...Exclude) {
	randInt := min + rand.Intn(max)
	for _, e := range exclude {
		if randInt >= e.Start && randInt <= e.End {
			randInt += 1
		}
	}
}

func RandomToken(length int) string {
	bytes := make([]byte, length)
	rand.Seed(time.Now().UnixNano())
	for i := 0; i < length; i++ {
		random.Seed(time.Now().UnixNano())
		op := random.RangeIntInclude(random.Slice{Start: 48, End: 57},
			random.Slice{Start: 65, End: 90}, random.Slice{Start: 97, End: 122})
		bytes[i] = byte(op) //A=65 and Z = 65+25
	}
	return string(bytes)
}

func RandomString(length ...int) string {

	randomLength := randomLength
	if len(length) > 0 {
		randomLength = length[0]
	}
	bytes := make([]byte, randomLength)
	rand.Seed(time.Now().UnixNano())
	for i := 0; i < randomLength; i++ {
		bytes[i] = byte(97 + rand.Intn(25)) //A=65 and Z = 65+25
	}
	return string(bytes)
}

func AcceptUserPassword(input Input) (string, error) {
start:
	fmt.Print(input.Tip)
	bytePassword, err := terminal.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return "", err
	}

	r := regexp.MustCompile(input.Regex)
	if !r.MatchString(string(bytePassword[:])) {
		log.Error("password format not correct,try again")
		goto start
	}

	fmt.Print("enter again:")
	bytePassword2, err := terminal.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return "", err
	}
	if len(bytePassword2) != len(bytePassword) {
		log.Error("password length not match, please try again")
		goto start
	}
	for k, v := range bytePassword {
		if bytePassword2[k] != v {
			log.Error("password not match, please try again")
			goto start
		}
	}

	log.Info("waiting...")

	return string(bytePassword[:]), nil
}