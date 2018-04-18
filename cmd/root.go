// Copyright © 2018 guilhem@barpilot.io
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
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"k8s.io/client-go/util/homedir"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	applogger "github.com/spotahome/kooper/log"

	"github.com/barpilot/node-labeler-operator/operator"
)

var cfgFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "node-labeler-operator",
	Short: "A kubernete operator to manage label/taints/annotations on nodes",
	Long: `node-labeler-operator manage node attributes based on labels on node.
	This is useful to tag specific node based on autogenerated attributes:
	kubernetes.io/hostname
	beta.kubernetes.io/os
	...`,

	RunE: run,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.node-labeler-operator.yaml)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

	// Get the user kubernetes configuration in it's home directory.
	kubehome := filepath.Join(homedir.HomeDir(), ".kube", "config")
	rootCmd.PersistentFlags().String("kubeconfig", kubehome, "Path to a kubeconfig. Only required if out-of-cluster.")
	viper.BindPFlag("kubeconfig", rootCmd.PersistentFlags().Lookup("kubeconfig"))
	rootCmd.PersistentFlags().String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	viper.BindPFlag("master", rootCmd.PersistentFlags().Lookup("master"))

	rootCmd.Flags().Int("resync-seconds", 30, "The number of seconds the controller will resync the resources")
	viper.BindPFlag("resync-seconds", rootCmd.Flags().Lookup("resync-seconds"))
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home := homedir.HomeDir()

		// Search config in home directory with name ".node-labeler-operator" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigName(".node-labeler-operator")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}
}

// Run runs the app.
func run(cmd *cobra.Command, args []string) error {
	logger := &applogger.Std{}

	// Get kubernetes rest client.
	nlCli, crdCli, k8sCli, err := GetKubernetesClients(logger)
	if err != nil {
		return err
	}

	// Create the operator and run
	oconfig := operator.NewOperatorConfig(time.Duration(viper.GetInt("resync-seconds")) * time.Second)
	op, err := operator.New(oconfig, nlCli, crdCli, k8sCli, logger)
	if err != nil {
		return err
	}

	stopC := make(chan struct{})
	finishC := make(chan error)
	signalC := make(chan os.Signal, 1)
	signal.Notify(signalC, syscall.SIGTERM, syscall.SIGINT)

	// Run in background the operator.
	go func() {
		finishC <- op.Run(stopC)
	}()

	select {
	case err := <-finishC:
		if err != nil {
			return err
		}
	case <-signalC:
		logger.Infof("Signal captured, exiting...")
	}
	return nil
}
