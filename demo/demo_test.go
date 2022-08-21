package demo

import (
	"context"
	"testing"
	
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

func TestName(t *testing.T) {
	kubeConfigPath := "/Users/zhengyansheng/.kube/config"
	restClient, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	if err != nil {
		panic(err)
	}
	
	clientSet, err := kubernetes.NewForConfig(restClient)
	if err != nil {
		panic(err)
	}
	
	dy, err := clientSet.AppsV1().Deployments(v1.NamespaceDefault).List(context.TODO(), v1.ListOptions{})
	if err != nil {
		panic(err)
	}
	klog.Infof("dy: %+v", dy)
}
