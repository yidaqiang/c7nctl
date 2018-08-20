// Copyright © 2018 VinkDong <dong@wenqi.us>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"github.com/spf13/cobra"
	"github.com/choerodon/c7n/cmd/app"
	"os"
	"fmt"
)

// installCmd represents the install command
var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install Choerodon",
	Long: `Install Choerodon quickly.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(InstallFile)
		if !app.CheckResource(InstallFile){
			os.Exit(1)
		}
		fmt.Print("install succeed")
		return nil
	},
}

var (
	ConfigFile string
	InstallFile string
)

func init() {
	installCmd.Flags().StringVarP(&InstallFile,"install-file","i","","Install Config file to read from")
	installCmd.Flags().StringVarP(&ConfigFile,"config-file","f","","Config file to read from")
	rootCmd.AddCommand(installCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// installCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// installCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
