/*
Copyright 2019 microsoft.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package notebookjob

import (
	"testing"
	"time"

	microsoftv1beta1 "microsoft/azure-databricks-operator/databricks-operator/pkg/apis/microsoft/v1beta1"
	mocks "microsoft/azure-databricks-operator/databricks-operator/pkg/mocks"
	randStr "microsoft/azure-databricks-operator/databricks-operator/pkg/rand"
	swagger "microsoft/azure-databricks-operator/databricks-operator/pkg/swagger"

	"github.com/onsi/gomega"
	"golang.org/x/net/context"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var c client.Client

var namespacedName = types.NamespacedName{Name: randStr.String(10), Namespace: "default"}
var expectedRequest = reconcile.Request{NamespacedName: namespacedName}

const timeout = time.Second * 120

func TestReconcile(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	secretData := make(map[string][]byte)
	secretData["eventHubName"] = []byte("ehname123")
	secretData["connectionString"] = []byte("Endpoint=sb://xxxx.servicebus.windows.net/;SharedAccessKeyName=xxxx;SharedAccessKey=xxxx")

	secretInput := &v1.Secret{Data: secretData, ObjectMeta: metav1.ObjectMeta{Name: "eventhub-input", Namespace: "default"}}
	secretOutput := &v1.Secret{Data: secretData, ObjectMeta: metav1.ObjectMeta{Name: "eventhub-output", Namespace: "default"}}

	instance := &microsoftv1beta1.NotebookJob{
		ObjectMeta: metav1.ObjectMeta{Name: namespacedName.Name, Namespace: namespacedName.Namespace},
		Spec: microsoftv1beta1.NotebookJobSpec{
			NotebookSpec: nil,
		},
	}

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	apiJobRuns := new(mocks.MockedApiJobRuns)

	apiClient := swagger.NewAPIClient(swagger.NewConfiguration())
	apiClient.ApijobsrunsApi = apiJobRuns
	recFn, requests := SetupTestReconcile(newReconcilerWithoutAPIClient(mgr, apiClient))
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		c.Delete(context.TODO(), secretInput)
		c.Delete(context.TODO(), secretOutput)
	}()

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	//kubectl create secret  generic myeh  --from-literal=eventHubName=ehname123 --from-literal=connectionString=Endpoint=sb://xxxx.servicebus.windows.net/;SharedAccessKeyName=xxxx;SharedAccessKey=xxxx
	c.Create(context.TODO(), secretInput)
	c.Create(context.TODO(), secretOutput)

	// Create the NotebookJob object and expect the Reconcile to be created
	err = c.Create(context.TODO(), instance)
	// The instance object may not be a valid object because it might be missing some required fields.
	// Please modify the instance object by adding required fields and then remove the following if statement.
	if apierrors.IsInvalid(err) {
		t.Logf("failed to create object, got an invalid object error: %v", err)
		return
	}
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
	g.Eventually(func() bool {
		_ = c.Get(context.TODO(), namespacedName, instance)
		return instance.HasFinalizer(finalizerName)
	}, timeout,
	).Should(gomega.BeTrue())

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
	g.Eventually(func() bool {
		_ = c.Get(context.TODO(), namespacedName, instance)
		return instance.IsRunning()
	}, timeout,
	).Should(gomega.BeTrue())

	err = c.Delete(context.TODO(), instance)
	if err != nil {
		t.Logf("failed to delete object: %v", err)
		return
	}

	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
	g.Eventually(func() error { return c.Delete(context.TODO(), instance) }, timeout).
		Should(gomega.MatchError("notebookjobs.microsoft.k8s.io \"" + namespacedName.Name + "\" not found"))
}
