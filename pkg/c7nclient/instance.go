package c7nclient

import (
	"fmt"
	"github.com/choerodon/c7n/pkg/c7nclient/model"
	"io"
)

func (c *C7NClient) ListEnvsInstance(out io.Writer,envId int) {
	if c.config.ProjectId == -1 {
		fmt.Printf("Set project Id")
		return
	}
	body := make(map[string]interface{})
	body["param"] = ""
	body["searchParam"] = make(map[string]string)
	req,err := c.newRequest("POST",fmt.Sprintf("/devops/v1/projects/%d/app_instances/%d/listByEnv",c.config.ProjectId,envId),nil,body)
	if err != nil {
		fmt.Printf("build request error")
	}
	var envInstanceList = model.DevopsEnvInstance{}
	_,err = c.do(req,&envInstanceList)
	if err != nil {
		fmt.Printf("request err:%v",err)
		return

	}
	envInstances := []model.EnvInstanceInfo{}
	for _,app := range envInstanceList.DevopsEnvPreviewApp {
		for _,appInstance := range app.ApplicationInstanceDTOS {
			instance := model.EnvInstanceInfo{
				AppCode: app.AppCode,
				AppName: app.AppName,
				InstanceCode: appInstance.Code,
				PodPreviewCount: fmt.Sprintf("%d/%d", appInstance.PodRunningCount, appInstance.PodCount),
				Status:  appInstance.Status,
				Version: appInstance.AppVersion,
				Id:  appInstance.ID,
			}
			envInstances = append(envInstances, instance)
		}

	}
	model.PrintEnvInstanceInfo(envInstances, out)

}

// devops/v1/projects/42/app_instances/5324/value

func (c *C7NClient) InstanceConfig(out io.Writer,instancesId int) {
	if c.config.ProjectId == -1 {
		fmt.Printf("Set project Id")
		return
	}
	req,err := c.newRequest("GET",fmt.Sprintf("/devops/v1/projects/%d/app_instances/%d/resources",c.config.ProjectId,instancesId),nil,nil)
	if err != nil {
		fmt.Printf("build request error")
	}
	var resp = model.InstanceResources{}
	_,err = c.do(req,&resp)
	if err != nil {
		return

	}
	model.PrintInstanceResources(resp,out)

}

// devops/v1/projects/42/app_instances/5324/value

func (c *C7NClient) InstanceResources(out io.Writer,instancesId int) {
	if c.config.ProjectId == -1 {
		fmt.Printf("Set project Id")
		return
	}
	req,err := c.newRequest("GET",fmt.Sprintf("devops/v1/projects/%d/app_instances/%d/value",c.config.ProjectId,instancesId),nil,nil)
	if err != nil {
		fmt.Printf("build request error")
	}
	var resp = model.InstanceValues{}
	_,err = c.do(req,&resp)
	if err != nil {
		fmt.Printf("request err:%v",err)
		return

	}
	fmt.Printf("The values of the instance:\n")
	fmt.Printf(resp.Values)

}


