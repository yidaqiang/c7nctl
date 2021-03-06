package action

import (
	"context"
	"encoding/json"
	"fmt"
	c7nclient "github.com/choerodon/c7nctl/pkg/client"
	c7ncfg "github.com/choerodon/c7nctl/pkg/config"
	c7nconsts "github.com/choerodon/c7nctl/pkg/consts"
	"github.com/choerodon/c7nctl/pkg/resource"
	"github.com/choerodon/c7nctl/pkg/slaver"
	c7nutils "github.com/choerodon/c7nctl/pkg/utils"
	std_errors "github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	yaml_v2 "gopkg.in/yaml.v2"
	"io/ioutil"
	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/kubernetes/pkg/util/maps"
	"k8s.io/kubernetes/staging/src/k8s.io/apimachinery/pkg/api/errors"
	"os"
	"sync"
)

type Choerodon struct {
	Cfg        *C7nConfiguration
	Metrics    resource.Metrics
	Slaver     *slaver.Slaver
	UserConfig *c7ncfg.C7nConfig

	Wg *sync.WaitGroup
	// TODO 是否移动到 cmd/c7nctl
	// api versions
	Version string
	// Choerodon version
	PassVersion string
	// choerodon install configuration
	ConfigFile string
	// install resource
	ResourceFile       string
	Prefix             string
	NoTimeout          bool
	SkipInput          bool
	RepoUrl            string
	Namespace          string
	Timeout            int
	Mail               string
	CommonLabels       map[string]string
	DefaultAccessModes []v1.PersistentVolumeAccessMode `yaml:"accessModes"`
}

func NewChoerodon(cfg *C7nConfiguration) *Choerodon {
	return &Choerodon{
		Cfg: cfg,
		CommonLabels: map[string]string{
			c7nconsts.C7nLabelKey: c7nconsts.C7nLabelValue,
		},
		Wg: &sync.WaitGroup{},
	}
}

func (c *Choerodon) InstallRelease(rls *resource.Release, vals map[string]interface{}) error {
	ti, err := c.Cfg.KubeClient.GetTaskInfoFromCM(c.Namespace, rls.Name)
	if err != nil {
		return err
	}

	if ti.Status == c7nconsts.SucceedStatus {
		log.Infof("Release %s is already installed", rls.Name)
		return nil
	}

	if ti.Status == c7nconsts.RenderedStatus || ti.Status == c7nconsts.FailedStatus {
		// 等待依赖项安装完成
		for _, r := range rls.Requirements {
			rls.CheckReleasePodRunning(r)
		}
		if err := rls.ExecutePreCommands(c.Slaver); err != nil {
			ti.Status = c7nconsts.FailedStatus
			return std_errors.WithMessage(err, fmt.Sprintf("Release %s execute pre commands failed", rls.Name))
		}

		args := c7nclient.ChartArgs{
			RepoUrl:     c.RepoUrl,
			Namespace:   c.Namespace,
			ReleaseName: c.getReleaseName(rls.Name),
			ChartName:   rls.Chart,
			Version:     rls.Version,
		}

		log.Infof("installing %s", rls.Name)
		// TODO 使用统一的 io.writer
		// 使用 upgrade --install cmd
		_, err := c.Cfg.HelmClient.Upgrade(args, vals, os.Stdout)
		if err != nil {
			ti.Status = c7nconsts.FailedStatus
			return err
		}
		ti.Status = c7nconsts.InstalledStatus
		// 将异步的 afterInstall 改为同步，AfterInstall 其依赖检查依靠 release
		if len(rls.AfterInstall) > 0 {
			if err := rls.ExecuteAfterTasks(c.Slaver, c.Wg); err != nil {
				ti.Status = c7nconsts.FailedStatus
				return std_errors.WithMessage(err, "Execute after task failed")
			}
		}
		ti.Status = c7nconsts.SucceedStatus
		log.Infof("Successfully installed %s", rls.Name)
	}
	// 完成后更新 task 状态
	return c.Cfg.KubeClient.SaveTaskInfoToCM(c.Namespace, ti)
}

func (c *Choerodon) InstallComponent(cname string) error {
	c.Version = c7nutils.GetVersion(c.Version)
	// TODO
	id, _ := c.GetInstallDef("", c7nconsts.DefaultResource)

	for _, rls := range id.Spec.Component {
		if rls.Name == cname {
			//renderComponent(rls)

			rls.Name = rls.Name + "-" + c7nutils.RandomString(5)
			if err := rls.InstallComponent(); err != nil {
				return err
			} else {
				break
			}
		}
	}
	return nil
}

func (c *Choerodon) CheckReleaseDomain(rls *resource.Release) error {
	for _, v := range rls.Values {
		if v.Check == "clusterdomain" {
			log.Debugf("Value %s: %s, checking: %s", v.Name, v.Value, v.Check)
			if err := c.Slaver.CheckClusterDomain(v.Value); err != nil {
				log.Errorf("请检查您的域名: %s 已正确解析到集群", v.Value)
				return err
			}
		}
	}
	return nil
}

// 为了避免循环依赖，从 resource.install.go 移到这里
func (c *Choerodon) GetInstallDef(cfg, res string) (*resource.InstallDefinition, error) {
	userConfig, err := GetUserConfig(cfg)
	if err != nil {
		return nil, std_errors.WithMessage(err, "Failed to get user config file")
	}
	c.UserConfig = userConfig

	// 在 getVersion 之后执行，已经确保了 i.Version 一定有值
	rd, err := c7nutils.GetInstallDefinition(res, c.Version)
	if err != nil {
		return nil, err
	}

	installDef := &resource.InstallDefinition{
		Namespace:    c.Namespace,
		Prefix:       c.Prefix,
		Timeout:      c.Timeout,
		SkipInput:    c.SkipInput,
		StorageClass: c.UserConfig.Spec.Persistence.StorageClassName,
		Version:      c.Version,
		CommonLabels: c.CommonLabels,
	}
	rdJson, err := yaml.ToJSON(rd)
	if err != nil {
		panic(err)
	}
	// slaver 使用了 core_v1.ContainerPort, 必须先转 JSON
	_ = json.Unmarshal(rdJson, installDef)

	installDef.PaaSVersion = c.Version
	if c.NoTimeout {
		installDef.Timeout = 60 * 60 * 24
	}

	if c.UserConfig != nil {
		if accessModes := c.UserConfig.Spec.Persistence.AccessModes; len(accessModes) > 0 {
			installDef.DefaultAccessModes = accessModes
		} else {
			installDef.DefaultAccessModes = []v1.PersistentVolumeAccessMode{"ReadWriteOnce"}
		}
	}

	return installDef, nil
}

func GetUserConfig(filePath string) (*c7ncfg.C7nConfig, error) {
	if filePath == "" {
		return nil, std_errors.New("No user config defined by `-c`")
	}
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, std_errors.WithMessage(err, "Read config file failed")
	}

	userConfig := &c7ncfg.C7nConfig{}
	if err = yaml_v2.Unmarshal(data, userConfig); err != nil {
		return nil, std_errors.WithMessage(err, "Unmarshal config failed")
	}
	log.WithField("profile", filePath).Info("The user profile was read successfully")
	return userConfig, nil
}

func (c *Choerodon) RenderReleases(id *resource.InstallDefinition) error {
	for _, rls := range id.Spec.Release {
		rls.Client = c.Cfg.KubeClient
		rls.Namespace = c.Namespace
		rls.Prefix = c.Prefix

		if err := id.RenderRelease(rls, c.UserConfig); err != nil {
			return err
		}
		// 检测域名
		if err := c.CheckReleaseDomain(rls); err != nil {
			return std_errors.WithMessage(err, fmt.Sprintf("Release %s's domain is invalid", rls.Name))
		}
	}
	return nil
}

func (c *Choerodon) RenderGitlabRunner(id *resource.InstallDefinition) error {
	rls := id.Spec.Runner
	rls.Client = c.Cfg.KubeClient
	rls.Namespace = c.Namespace
	rls.Prefix = c.Prefix
	for _, p := range rls.Persistence {
		if _, err := c.PreparePvc(p); err != nil {
			return err
		}
	}

	return id.RenderRelease(rls, c.UserConfig)
}

func (c *Choerodon) Clean() error {
	if err := c.CleanJobs(); err != nil {
		return err
	}

	return nil
}

func (c *Choerodon) cleanConfigMaps() error {
	return c.Cfg.KubeClient.DeleteCM(c.Namespace, c7nconsts.StaticLogsCM)
}

func (c *Choerodon) cleanSlaver() error {
	if err := c.Cfg.KubeClient.DeleteDaemonSet(c.Namespace, c.Slaver.Name); err != nil {
		return err
	}
	if err := c.Cfg.KubeClient.DeletePvc(c.Namespace, c.Slaver.PvcName); err != nil {
		return err
	}
	return nil
}

// mv to client package
func (c *Choerodon) CleanJobs() error {
	jobInterface := c.Cfg.KubeClient.GetClientSet().BatchV1().Jobs(c.Namespace)
	jobList, err := jobInterface.List(context.TODO(), meta_v1.ListOptions{})
	if err != nil {
		return err
	}
	log.Info("clean history jobs...")
	delOpts := meta_v1.DeleteOptions{}
	for _, job := range jobList.Items {
		if job.Status.Active > 0 {
			log.Infof("job %s still active ignored..", job.Name)
		} else {
			if err := jobInterface.Delete(context.TODO(), job.Name, delOpts); err != nil {
				return err
			}
			log.Infof("deleted job %s", job.Name)
		}
		log.Info(job.Name)
	}
	return nil
}

func (c *Choerodon) CheckResource(resources *v1.ResourceRequirements) error {
	request := resources.Requests

	reqMemory := request.Memory().Value()
	reqCpu := request.Cpu().Value()
	clusterMemory, clusterCpu := c.Cfg.KubeClient.GetClusterResource()

	c.Metrics.Memory = clusterMemory
	c.Metrics.CPU = clusterCpu

	serverVersion, err := c.Cfg.KubeClient.GetServerVersion()
	if err != nil {
		return std_errors.Wrap(err, "can't get your cluster version")
	}
	c.Metrics.Version = serverVersion.String()
	if clusterMemory < reqMemory {
		return std_errors.New(fmt.Sprintf("cluster memory not enough, request %dGi", reqMemory/(1024*1024*1024)))
	}
	if clusterCpu < reqCpu {
		return std_errors.New(fmt.Sprintf("cluster cpu not enough, request %dc", reqCpu/1000))
	}
	return nil
}

func (c *Choerodon) CheckNamespace(namespace string) error {
	_, err := c.Cfg.KubeClient.GetNamespace(namespace)
	if err != nil {
		if errors.IsNotFound(err) {
			return c.Cfg.KubeClient.CreateNamespace(namespace)
		}
		return err
	}
	log.Infof("namespace %s already exists", namespace)
	return nil
}

/**
  创建 slaver 的相关操作
*/
func (c *Choerodon) PrepareSlaver(stopCh <-chan struct{}) (*slaver.Slaver, error) {
	// s.Client = c.cfg.KubeClient
	// be care of use point
	c.Slaver.CommonLabels = maps.CopySS(c.CommonLabels)
	c.Slaver.Namespace = c.Namespace
	c.Slaver.Client = c.Cfg.KubeClient.GetClientSet()

	if pvcName, err := c.prepareSlaverPvc(&c.UserConfig.Spec.Persistence); err != nil {
		return c.Slaver, err
	} else {
		c.Slaver.PvcName = pvcName
	}

	if _, err := c.Slaver.CheckInstall(); err != nil {
		return c.Slaver, err
	}
	port := c.Slaver.ForwardPort("http", stopCh)
	grpcPort := c.Slaver.ForwardPort("grpc", stopCh)
	c.Slaver.Address = fmt.Sprintf("http://127.0.0.1:%d", port)
	c.Slaver.GRpcAddress = fmt.Sprintf("127.0.0.1:%d", grpcPort)
	return c.Slaver, nil
}

/**
 */
func (c *Choerodon) prepareSlaverPvc(p *c7ncfg.Persistence) (string, error) {
	if c.UserConfig == nil {
		return "", nil
	}
	//pvs := c.UserConfig.Spec.Persistence.GetPersistentVolumeSource("")

	persistence := resource.Persistence{
		Namespace:    c.Namespace,
		Client:       c.Cfg.KubeClient,
		CommonLabels: c.CommonLabels,
		AccessModes:  c.DefaultAccessModes,
		Size:         "1Gi",
		Mode:         "755",
		PvcEnabled:   true,
		Name:         "slaver",
		StorageClass: c.UserConfig.Spec.Persistence.StorageClassName,
	}

	// 基于 nfs StorageClass 自动创建 PV
	if err := persistence.CheckOrCreatePvc(p.StorageClassName); err != nil {
		return "", err
	}
	return persistence.RefPvcName, nil
}

func (c *Choerodon) SendMetrics(err error) {

}

func (c *Choerodon) getReleaseName(rlsName string) string {
	if c.Prefix != "" {
		rlsName = fmt.Sprintf("%s-%s", c.Prefix, rlsName)
	}
	return rlsName
}

func (c *Choerodon) PreparePvc(persistence *resource.Persistence) (string, error) {
	persistence.Namespace = c.Namespace
	persistence.CommonLabels = c.CommonLabels
	persistence.Client = c.Cfg.KubeClient
	persistence.CommonLabels = c.CommonLabels
	persistence.StorageClass = c.UserConfig.Spec.Persistence.StorageClassName
	/*
		err := persistence.CheckOrCreatePv(p)
		if err != nil {
			return "", err
		}
	*/
	// 基于 nfs StorageClass 自动创建 PV
	if err := persistence.CheckOrCreatePvc(c.UserConfig.Spec.Persistence.StorageClassName); err != nil {
		return "", err
	}
	return persistence.RefPvcName, nil
}
