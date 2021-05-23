package main

import (
	"context"
	"fmt"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/d4l3k/messagediff.v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

type Param struct {
	Name      string
	Shorthand string
	Value     interface{}
	Usage     string
	Required  bool
}

var rootParams = []Param{
	{Name: "verbose", Shorthand: "v", Value: false, Usage: "--verbose=true|false"},
	{Name: "match-label", Shorthand: "", Value: "mlops.cnvrg.io", Usage: "label to use for matching"},
	{Name: "json-log", Shorthand: "J", Value: false, Usage: "--json-log=true|false"},
	{Name: "kubeconfig", Shorthand: "", Value: kubeconfigDefaultLocation(), Usage: "absolute path to the kubeconfig file"},
}

var rootCmd = &cobra.Command{
	Use:   "cre",
	Short: "cre - config reloader for K8s",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		setupLogging()

	},
	Run: func(cmd *cobra.Command, args []string) {
		logrus.Info("starting cre...")
		stopper := make(chan bool)
		defer close(stopper)
		go cmInformer()
		go secretInformer()
		<-stopper
	},
}

func setupLogging() {

	// Set log verbosity
	if viper.GetBool("verbose") {
		logrus.SetLevel(logrus.DebugLevel)
		logrus.SetReportCaller(true)
	} else {
		logrus.SetLevel(logrus.InfoLevel)
		logrus.SetReportCaller(false)
	}
	if viper.GetBool("json-log") {
		logrus.SetFormatter(&logrus.JSONFormatter{})
	} else {
		logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})
	}
	// Logs are always goes to STDOUT
	logrus.SetOutput(os.Stdout)
}

func setParams(params []Param, command *cobra.Command) {
	for _, param := range params {
		switch v := param.Value.(type) {
		case int:
			command.PersistentFlags().IntP(param.Name, param.Shorthand, v, param.Usage)
		case string:
			command.PersistentFlags().StringP(param.Name, param.Shorthand, v, param.Usage)
		case bool:
			command.PersistentFlags().BoolP(param.Name, param.Shorthand, v, param.Usage)
		}
		if err := viper.BindPFlag(param.Name, command.PersistentFlags().Lookup(param.Name)); err != nil {
			panic(err)
		}
	}
}

func setupCommands() {
	// Init config
	cobra.OnInitialize(initConfig)
	setParams(rootParams, rootCmd)

}

func kubeconfigDefaultLocation() string {
	kubeconfigDefaultLocation := ""
	if home := homedir.HomeDir(); home != "" {
		kubeconfigDefaultLocation = filepath.Join(home, ".kube", "config")
	}
	return kubeconfigDefaultLocation
}

func initConfig() {
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
}

func clientset() *kubernetes.Clientset {
	if _, err := os.Stat(viper.GetString("kubeconfig")); os.IsNotExist(err) {
		config, err := rest.InClusterConfig()
		if err != nil {
			panic(err.Error())
		}
		clientset, err := kubernetes.NewForConfig(config)
		if err != nil {
			panic(err.Error())
		}
		return clientset
	} else if err != nil {
		logrus.Fatalf("%s failed to check kubeconfig location", err)
	}

	config, err := clientcmd.BuildConfigFromFlags("", viper.GetString("kubeconfig"))
	if err != nil {
		panic(err.Error())
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	return clientset

}

func secretInformer() {
	matchLabel := viper.GetString("match-label")
	logrus.Infof("starting Secrets Informer, match-label: %s", matchLabel)
	factory := informers.NewSharedInformerFactory(clientset(), 0)
	informer := factory.Core().V1().Secrets().Informer()
	stopper := make(chan struct{})
	defer close(stopper)
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldO := oldObj.(*corev1.Secret)
			newO := newObj.(*corev1.Secret)
			if _, ok := oldO.Labels[matchLabel]; !ok {
				return
			}
			if !reflect.DeepEqual(oldO.Data, newO.Data) || !reflect.DeepEqual(oldO.StringData, newO.StringData) {
				diff, _ := messagediff.PrettyDiff(oldO.Data, newO.Data)
				logrus.Infof("Data diff: %s", diff)
				diff, _ = messagediff.PrettyDiff(oldO.StringData, newO.StringData)
				logrus.Infof("String Data diff: %s", diff)
				logrus.Infof("going to rollout resources labeld with %s:%s", matchLabel, oldO.Labels[matchLabel])
				rollout(oldO.Namespace, oldO.Labels[matchLabel])
			}
		},
	})
	informer.Run(stopper)
}

func cmInformer() {
	matchLabel := viper.GetString("match-label")
	logrus.Infof("starting ConfigMap Informer, match-label: %s", matchLabel)
	factory := informers.NewSharedInformerFactory(clientset(), 0)
	informer := factory.Core().V1().ConfigMaps().Informer()
	stopper := make(chan struct{})
	defer close(stopper)
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldO := oldObj.(*corev1.ConfigMap)
			newO := newObj.(*corev1.ConfigMap)
			if _, ok := oldO.Labels[matchLabel]; !ok {
				return
			}
			if !reflect.DeepEqual(oldO.Data, newO.Data) {
				diff, _ := messagediff.PrettyDiff(oldO.Data, newO.Data)
				logrus.Infof("%s", diff)
				logrus.Infof("going to rollout resources labeld with %s:%s", matchLabel, oldO.Labels[matchLabel])
				rollout(oldO.Namespace, oldO.Labels[matchLabel])
			}
		},
	})
	informer.Run(stopper)
}

func rollout(ns string, matchLabelValue string) {
	rolloutDeployments(ns, matchLabelValue)
	rolloutStatefulSets(ns, matchLabelValue)
	rolloutDaemonSets(ns, matchLabelValue)
}

func rolloutDeployments(ns string, matchLabelValue string) {
	clientset := clientset()
	matchLabel := viper.GetString("match-label")
	listOptions := metav1.ListOptions{
		LabelSelector: matchLabel,
	}
	deploymentList, err := clientset.AppsV1().Deployments(ns).List(context.Background(), listOptions)
	if err != nil {
		logrus.Error(err)
		logrus.Fatalf("failed to list deployments in namespace: %s ", ns)
	}

	for _, deployment := range deploymentList.Items {
		if _, ok := deployment.Labels[matchLabel]; ok {
			if deployment.Labels[matchLabel] == matchLabelValue {
				triggerDeploymentRollout(ns, deployment.Name)
			}
		}
	}
}

func rolloutStatefulSets(ns string, matchLabelValue string) {
	clientset := clientset()
	matchLabel := viper.GetString("match-label")
	listOptions := metav1.ListOptions{
		LabelSelector: matchLabel,
	}
	deploymentList, err := clientset.AppsV1().StatefulSets(ns).List(context.Background(), listOptions)
	if err != nil {
		logrus.Error(err)
		logrus.Fatalf("failed to list deployments in namespace: %s ", ns)
	}

	for _, deployment := range deploymentList.Items {
		if _, ok := deployment.Labels[matchLabel]; ok {
			if deployment.Labels[matchLabel] == matchLabelValue {
				triggerStatefulRollout(ns, deployment.Name)
			}
		}
	}
}

func rolloutDaemonSets(ns string, matchLabelValue string) {
	clientset := clientset()
	matchLabel := viper.GetString("match-label")
	listOptions := metav1.ListOptions{
		LabelSelector: matchLabel,
	}
	deploymentList, err := clientset.AppsV1().DaemonSets(ns).List(context.Background(), listOptions)
	if err != nil {
		logrus.Error(err)
		logrus.Fatalf("failed to list DaemonSets in namespace: %s ", ns)
	}

	for _, deployment := range deploymentList.Items {
		if _, ok := deployment.Labels[matchLabel]; ok {
			if deployment.Labels[matchLabel] == matchLabelValue {
				triggerDaemonsetRollout(ns, deployment.Name)
			}
		}
	}
}

func triggerDeploymentRollout(ns string, deploymentName string) {
	clientset := clientset()
	data := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"%s"}}}}}`, time.Now().String())
	_, err := clientset.
		AppsV1().
		Deployments(ns).
		Patch(context.Background(), deploymentName, types.StrategicMergePatchType, []byte(data), metav1.PatchOptions{FieldManager: "cnvrg-cre-rollout"})
	if err != nil {
		logrus.Error(err)
		logrus.Fatalf("error triggering deployment rolout")
	}
}

func triggerStatefulRollout(ns string, deploymentName string) {
	clientset := clientset()
	data := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"%s"}}}}}`, time.Now().String())
	_, err := clientset.
		AppsV1().
		StatefulSets(ns).
		Patch(context.Background(), deploymentName, types.StrategicMergePatchType, []byte(data), metav1.PatchOptions{FieldManager: "cnvrg-cre-rollout"})
	if err != nil {
		logrus.Error(err)
		logrus.Fatalf("error triggering statefulset rolout")
	}
}

func triggerDaemonsetRollout(ns string, deploymentName string) {
	clientset := clientset()
	data := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"%s"}}}}}`, time.Now().String())
	_, err := clientset.
		AppsV1().
		DaemonSets(ns).
		Patch(context.Background(), deploymentName, types.StrategicMergePatchType, []byte(data), metav1.PatchOptions{FieldManager: "cnvrg-cre-rollout"})
	if err != nil {
		logrus.Error(err)
		logrus.Fatalf("error triggering statefulset rolout")
	}
}

func main() {
	setupCommands()
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
