/*

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

package controllers

import (
	"context"
	"fmt"
	"strings"
	"time"

	memcachedv1 "github.com/openstack-k8s-operators/infra-operator/apis/memcached/v1beta1"
	rabbitmqv1 "github.com/openstack-k8s-operators/infra-operator/apis/rabbitmq/v1beta1"
	keystonev1 "github.com/openstack-k8s-operators/keystone-operator/api/v1beta1"
	keystone "github.com/openstack-k8s-operators/keystone-operator/pkg/keystone"
	"github.com/openstack-k8s-operators/lib-common/modules/common"
	condition "github.com/openstack-k8s-operators/lib-common/modules/common/condition"
	configmap "github.com/openstack-k8s-operators/lib-common/modules/common/configmap"
	cronjob "github.com/openstack-k8s-operators/lib-common/modules/common/cronjob"
	deployment "github.com/openstack-k8s-operators/lib-common/modules/common/deployment"
	"github.com/openstack-k8s-operators/lib-common/modules/common/endpoint"
	env "github.com/openstack-k8s-operators/lib-common/modules/common/env"
	helper "github.com/openstack-k8s-operators/lib-common/modules/common/helper"
	job "github.com/openstack-k8s-operators/lib-common/modules/common/job"
	labels "github.com/openstack-k8s-operators/lib-common/modules/common/labels"
	nad "github.com/openstack-k8s-operators/lib-common/modules/common/networkattachment"
	common_rbac "github.com/openstack-k8s-operators/lib-common/modules/common/rbac"
	"github.com/openstack-k8s-operators/lib-common/modules/common/secret"
	oko_secret "github.com/openstack-k8s-operators/lib-common/modules/common/secret"
	"github.com/openstack-k8s-operators/lib-common/modules/common/service"
	"github.com/openstack-k8s-operators/lib-common/modules/common/tls"
	util "github.com/openstack-k8s-operators/lib-common/modules/common/util"
	mariadbv1 "github.com/openstack-k8s-operators/mariadb-operator/api/v1beta1"

	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8s_errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/utils/ptr"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// GetClient -
func (r *KeystoneAPIReconciler) GetClient() client.Client {
	return r.Client
}

// GetKClient -
func (r *KeystoneAPIReconciler) GetKClient() kubernetes.Interface {
	return r.Kclient
}

// GetScheme -
func (r *KeystoneAPIReconciler) GetScheme() *runtime.Scheme {
	return r.Scheme
}

// GetLog returns a logger object with a prefix of "conroller.name" and aditional controller context fields
func GetLog(ctx context.Context) logr.Logger {
	return log.FromContext(ctx).WithName("Controllers").WithName("KeystoneAPI")
}

// KeystoneAPIReconciler reconciles a KeystoneAPI object
type KeystoneAPIReconciler struct {
	client.Client
	Kclient kubernetes.Interface
	Scheme  *runtime.Scheme
}

// +kubebuilder:rbac:groups=keystone.openstack.org,resources=keystoneapis,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keystone.openstack.org,resources=keystoneapis/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=keystone.openstack.org,resources=keystoneapis/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete;
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete;
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete;
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete;
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete;
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete;
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;patch;delete;
// +kubebuilder:rbac:groups=mariadb.openstack.org,resources=mariadbdatabases,verbs=get;list;watch;create;update;patch;delete;
// +kubebuilder:rbac:groups=mariadb.openstack.org,resources=mariadbaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mariadb.openstack.org,resources=mariadbaccounts/finalizers,verbs=update
// +kubebuilder:rbac:groups=memcached.openstack.org,resources=memcacheds,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=memcached.openstack.org,resources=memcacheds/finalizers,verbs=update
// +kubebuilder:rbac:groups=k8s.cni.cncf.io,resources=network-attachment-definitions,verbs=get;list;watch
// +kubebuilder:rbac:groups=rabbitmq.openstack.org,resources=transporturls,verbs=get;list;watch;create;update;patch;delete

// service account, role, rolebinding
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=roles,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=rolebindings,verbs=get;list;watch;create;update
// keystone service account permissions that are needed to grant permission to the above
// +kubebuilder:rbac:groups="security.openshift.io",resourceNames=anyuid,resources=securitycontextconstraints,verbs=use
// +kubebuilder:rbac:groups="",resources=pods,verbs=create;delete;get;list;patch;update;watch

// Reconcile reconcile keystone API requests
func (r *KeystoneAPIReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, _err error) {
	// Fetch the KeystoneAPI instance
	instance := &keystonev1.KeystoneAPI{}
	err := r.Client.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if k8s_errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected.
			// For additional cleanup logic use finalizers. Return and don't requeue.
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	helper, err := helper.NewHelper(
		instance,
		r.Client,
		r.Kclient,
		r.Scheme,
		GetLog(ctx),
	)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Always patch the instance status when exiting this function so we can persist any changes.
	defer func() {
		// update the Ready condition based on the sub conditions
		if instance.Status.Conditions.AllSubConditionIsTrue() {
			instance.Status.Conditions.MarkTrue(
				condition.ReadyCondition, condition.ReadyMessage)
		} else {
			// something is not ready so reset the Ready condition
			instance.Status.Conditions.MarkUnknown(
				condition.ReadyCondition, condition.InitReason, condition.ReadyInitMessage)
			// and recalculate it based on the state of the rest of the conditions
			instance.Status.Conditions.Set(
				instance.Status.Conditions.Mirror(condition.ReadyCondition))
		}
		err := helper.PatchInstance(ctx, instance)
		if err != nil {
			_err = err
			return
		}
	}()

	// If we're not deleting this and the service object doesn't have our finalizer, add it.
	if instance.DeletionTimestamp.IsZero() && controllerutil.AddFinalizer(instance, helper.GetFinalizer()) {
		return ctrl.Result{}, err
	}

	//
	// initialize status
	//
	if instance.Status.Conditions == nil {
		instance.Status.Conditions = condition.Conditions{}

		cl := condition.CreateList(
			condition.UnknownCondition(condition.DBReadyCondition, condition.InitReason, condition.DBReadyInitMessage),
			condition.UnknownCondition(condition.DBSyncReadyCondition, condition.InitReason, condition.DBSyncReadyInitMessage),
			condition.UnknownCondition(condition.RabbitMqTransportURLReadyCondition, condition.InitReason, condition.RabbitMqTransportURLReadyInitMessage),
			condition.UnknownCondition(condition.MemcachedReadyCondition, condition.InitReason, condition.MemcachedReadyInitMessage),
			condition.UnknownCondition(condition.ExposeServiceReadyCondition, condition.InitReason, condition.ExposeServiceReadyInitMessage),
			condition.UnknownCondition(condition.BootstrapReadyCondition, condition.InitReason, condition.BootstrapReadyInitMessage),
			condition.UnknownCondition(condition.InputReadyCondition, condition.InitReason, condition.InputReadyInitMessage),
			condition.UnknownCondition(condition.ServiceConfigReadyCondition, condition.InitReason, condition.ServiceConfigReadyInitMessage),
			condition.UnknownCondition(condition.DeploymentReadyCondition, condition.InitReason, condition.DeploymentReadyInitMessage),
			condition.UnknownCondition(condition.NetworkAttachmentsReadyCondition, condition.InitReason, condition.NetworkAttachmentsReadyInitMessage),
			condition.UnknownCondition(condition.CronJobReadyCondition, condition.InitReason, condition.CronJobReadyInitMessage),
			condition.UnknownCondition(condition.TLSInputReadyCondition, condition.InitReason, condition.InputReadyInitMessage),
			// service account, role, rolebinding conditions
			condition.UnknownCondition(condition.ServiceAccountReadyCondition, condition.InitReason, condition.ServiceAccountReadyInitMessage),
			condition.UnknownCondition(condition.RoleReadyCondition, condition.InitReason, condition.RoleReadyInitMessage),
			condition.UnknownCondition(condition.RoleBindingReadyCondition, condition.InitReason, condition.RoleBindingReadyInitMessage),
		)

		instance.Status.Conditions.Init(&cl)

		// Register overall status immediately to have an early feedback e.g. in the cli
		return ctrl.Result{}, nil
	}
	if instance.Status.Hash == nil {
		instance.Status.Hash = map[string]string{}
	}
	if instance.Status.APIEndpoints == nil {
		instance.Status.APIEndpoints = map[string]string{}
	}
	if instance.Status.NetworkAttachments == nil {
		instance.Status.NetworkAttachments = map[string][]string{}
	}

	// Handle service delete
	if !instance.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, instance, helper)
	}

	// Handle non-deleted clusters
	return r.reconcileNormal(ctx, instance, helper)
}

// fields to index to reconcile when change
const (
	passwordSecretField     = ".spec.secret"
	caBundleSecretNameField = ".spec.tls.caBundleSecretName"
	tlsAPIInternalField     = ".spec.tls.api.internal.secretName"
	tlsAPIPublicField       = ".spec.tls.api.public.secretName"
)

var (
	allWatchFields = []string{
		passwordSecretField,
		caBundleSecretNameField,
		tlsAPIInternalField,
		tlsAPIPublicField,
	}
)

// SetupWithManager -
func (r *KeystoneAPIReconciler) SetupWithManager(mgr ctrl.Manager) error {
	logger := mgr.GetLogger()

	// index passwordSecretField
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &keystonev1.KeystoneAPI{}, passwordSecretField, func(rawObj client.Object) []string {
		// Extract the secret name from the spec, if one is provided
		cr := rawObj.(*keystonev1.KeystoneAPI)
		if cr.Spec.Secret == "" {
			return nil
		}
		return []string{cr.Spec.Secret}
	}); err != nil {
		return err
	}

	// index caBundleSecretNameField
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &keystonev1.KeystoneAPI{}, caBundleSecretNameField, func(rawObj client.Object) []string {
		// Extract the secret name from the spec, if one is provided
		cr := rawObj.(*keystonev1.KeystoneAPI)
		if cr.Spec.TLS.CaBundleSecretName == "" {
			return nil
		}
		return []string{cr.Spec.TLS.CaBundleSecretName}
	}); err != nil {
		return err
	}

	// index tlsAPIInternalField
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &keystonev1.KeystoneAPI{}, tlsAPIInternalField, func(rawObj client.Object) []string {
		// Extract the secret name from the spec, if one is provided
		cr := rawObj.(*keystonev1.KeystoneAPI)
		if cr.Spec.TLS.API.Internal.SecretName == nil {
			return nil
		}
		return []string{*cr.Spec.TLS.API.Internal.SecretName}
	}); err != nil {
		return err
	}

	// index tlsAPIPublicField
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &keystonev1.KeystoneAPI{}, tlsAPIPublicField, func(rawObj client.Object) []string {
		// Extract the secret name from the spec, if one is provided
		cr := rawObj.(*keystonev1.KeystoneAPI)
		if cr.Spec.TLS.API.Public.SecretName == nil {
			return nil
		}
		return []string{*cr.Spec.TLS.API.Public.SecretName}
	}); err != nil {
		return err
	}

	memcachedFn := func(o client.Object) []reconcile.Request {
		result := []reconcile.Request{}

		// get all KeystoneAPI CRs
		keystoneAPIs := &keystonev1.KeystoneAPIList{}
		listOpts := []client.ListOption{
			client.InNamespace(o.GetNamespace()),
		}
		if err := r.Client.List(context.Background(), keystoneAPIs, listOpts...); err != nil {
			logger.Error(err, "Unable to retrieve KeystoneAPI CRs %w")
			return nil
		}

		for _, cr := range keystoneAPIs.Items {
			if o.GetName() == cr.Spec.MemcachedInstance {
				name := client.ObjectKey{
					Namespace: o.GetNamespace(),
					Name:      cr.Name,
				}
				logger.Info(fmt.Sprintf("Memcached %s is used by KeystoneAPI CR %s", o.GetName(), cr.Name))
				result = append(result, reconcile.Request{NamespacedName: name})
			}
		}
		if len(result) > 0 {
			return result
		}
		return nil
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&keystonev1.KeystoneAPI{}).
		Owns(&mariadbv1.MariaDBDatabase{}).
		Owns(&mariadbv1.MariaDBAccount{}).
		Owns(&batchv1.Job{}).
		Owns(&batchv1.CronJob{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Watches(&source.Kind{Type: &memcachedv1.Memcached{}},
			handler.EnqueueRequestsFromMapFunc(memcachedFn)).
		Watches(
			&source.Kind{Type: &corev1.Secret{}},
			handler.EnqueueRequestsFromMapFunc(r.findObjectsForSrc),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Complete(r)
}

func (r *KeystoneAPIReconciler) findObjectsForSrc(src client.Object) []reconcile.Request {
	requests := []reconcile.Request{}

	l := log.FromContext(context.Background()).WithName("Controllers").WithName("KeystoneAPI")

	for _, field := range allWatchFields {
		crList := &keystonev1.KeystoneAPIList{}
		listOps := &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(field, src.GetName()),
			Namespace:     src.GetNamespace(),
		}
		err := r.List(context.TODO(), crList, listOps)
		if err != nil {
			return []reconcile.Request{}
		}

		for _, item := range crList.Items {
			l.Info(fmt.Sprintf("input source %s changed, reconcile: %s - %s", src.GetName(), item.GetName(), item.GetNamespace()))

			requests = append(requests,
				reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      item.GetName(),
						Namespace: item.GetNamespace(),
					},
				},
			)
		}
	}

	return requests
}

func (r *KeystoneAPIReconciler) reconcileDelete(ctx context.Context, instance *keystonev1.KeystoneAPI, helper *helper.Helper) (ctrl.Result, error) {
	l := GetLog(ctx)
	l.Info("Reconciling Service delete")

	// We need to allow all KeystoneEndpoint and KeystoneService processing to finish
	// in the case of a delete before we remove the finalizers.  For instance, in the
	// case of the Memcached dependency, if Memcached is deleted before all Keystone
	// cleanup has finished, then the Keystone logic will likely hit a 500 error and
	// thus its deletion will hang indefinitely.
	for _, finalizer := range instance.Finalizers {
		// If this finalizer is not our KeystoneAPI finalizer, then it is either
		// a KeystoneService or KeystoneEndpointer finalizer, which indicates that
		// there is more Keystone processing that needs to finish before we can
		// allow our DB and Memcached dependencies to be potentially deleted
		// themselves
		if finalizer != helper.GetFinalizer() {
			return ctrl.Result{}, nil
		}
	}

	// Remove our finalizer from Memcached
	memcached, err := r.getKeystoneMemcached(ctx, helper, instance)
	if err != nil && !k8s_errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	if !k8s_errors.IsNotFound(err) && memcached != nil {
		if controllerutil.RemoveFinalizer(memcached, helper.GetFinalizer()) {
			err := r.Update(ctx, memcached)

			if err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	// remove db finalizer before the keystone one
	db, err := mariadbv1.GetDatabaseByName(ctx, helper, instance.Name)
	if err != nil && !k8s_errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	if !k8s_errors.IsNotFound(err) {
		if err := db.DeleteFinalizer(ctx, helper); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Service is deleted so remove the finalizer.
	controllerutil.RemoveFinalizer(instance, helper.GetFinalizer())
	l.Info("Reconciled Service delete successfully")

	return ctrl.Result{}, nil
}

func (r *KeystoneAPIReconciler) reconcileInit(
	ctx context.Context,
	instance *keystonev1.KeystoneAPI,
	helper *helper.Helper,
	serviceLabels map[string]string,
	serviceAnnotations map[string]string,
) (ctrl.Result, error) {
	l := GetLog(ctx)
	l.Info("Reconciling Service init")

	//
	// Service account, role, binding
	//
	rbacRules := []rbacv1.PolicyRule{
		{
			APIGroups:     []string{"security.openshift.io"},
			ResourceNames: []string{"anyuid"},
			Resources:     []string{"securitycontextconstraints"},
			Verbs:         []string{"use"},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"pods"},
			Verbs:     []string{"create", "get", "list", "watch", "update", "patch", "delete"},
		},
	}
	rbacResult, err := common_rbac.ReconcileRbac(ctx, helper, instance, rbacRules)
	if err != nil {
		return rbacResult, err
	} else if (rbacResult != ctrl.Result{}) {
		return rbacResult, nil
	}

	//
	// create service DB instance
	//
	db := mariadbv1.NewDatabase(
		instance.Name,
		instance.Spec.DatabaseUser,
		instance.Spec.Secret,
		map[string]string{
			"dbName": instance.Spec.DatabaseInstance,
		},
	)
	// create or patch the DB
	ctrlResult, err := db.CreateOrPatchDB(
		ctx,
		helper,
	)
	if err != nil {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.DBReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			condition.DBReadyErrorMessage,
			err.Error()))
		return ctrl.Result{}, err
	}
	if (ctrlResult != ctrl.Result{}) {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.DBReadyCondition,
			condition.RequestedReason,
			condition.SeverityInfo,
			condition.DBReadyRunningMessage))
		return ctrlResult, nil
	}

	// wait for the DB to be setup
	ctrlResult, err = db.WaitForDBCreated(ctx, helper)
	if err != nil {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.DBReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			condition.DBReadyErrorMessage,
			err.Error()))
		return ctrlResult, err
	}
	if (ctrlResult != ctrl.Result{}) {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.DBReadyCondition,
			condition.RequestedReason,
			condition.SeverityInfo,
			condition.DBReadyRunningMessage))
		return ctrlResult, nil
	}
	// update Status.DatabaseHostname, used to bootstrap/config the service
	instance.Status.DatabaseHostname = db.GetDatabaseHostname()
	instance.Status.Conditions.MarkTrue(condition.DBReadyCondition, condition.DBReadyMessage)

	// create service DB - end

	//
	// run keystone db sync
	//
	dbSyncHash := instance.Status.Hash[keystonev1.DbSyncHash]
	jobDef := keystone.DbSyncJob(instance, serviceLabels, serviceAnnotations)
	dbSyncjob := job.NewJob(
		jobDef,
		keystonev1.DbSyncHash,
		instance.Spec.PreserveJobs,
		5*time.Second,
		dbSyncHash,
	)
	ctrlResult, err = dbSyncjob.DoJob(
		ctx,
		helper,
	)
	if (ctrlResult != ctrl.Result{}) {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.DBSyncReadyCondition,
			condition.RequestedReason,
			condition.SeverityInfo,
			condition.DBSyncReadyRunningMessage))
		return ctrlResult, nil
	}
	if err != nil {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.DBSyncReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			condition.DBSyncReadyErrorMessage,
			err.Error()))
		return ctrl.Result{}, err
	}
	if dbSyncjob.HasChanged() {
		instance.Status.Hash[keystonev1.DbSyncHash] = dbSyncjob.GetHash()
		l.Info(fmt.Sprintf("Job %s hash added - %s", jobDef.Name, instance.Status.Hash[keystonev1.DbSyncHash]))
	}
	instance.Status.Conditions.MarkTrue(condition.DBSyncReadyCondition, condition.DBSyncReadyMessage)

	// run keystone db sync - end

	//
	// create service/s
	//
	var keystoneEndpoints = map[service.Endpoint]endpoint.Data{
		service.EndpointPublic: {
			Port: keystone.KeystonePublicPort,
		},
		service.EndpointInternal: {
			Port: keystone.KeystoneInternalPort,
		},
	}

	apiEndpoints := make(map[string]string)
	for endpointType, data := range keystoneEndpoints {
		endpointTypeStr := string(endpointType)
		endpointName := instance.Name + "-" + endpointTypeStr

		svcOverride := instance.Spec.Override.Service[endpointType]
		if svcOverride.EmbeddedLabelsAnnotations == nil {
			svcOverride.EmbeddedLabelsAnnotations = &service.EmbeddedLabelsAnnotations{}
		}

		exportLabels := util.MergeStringMaps(
			serviceLabels,
			map[string]string{
				service.AnnotationEndpointKey: endpointTypeStr,
			},
		)

		// Create the service
		svc, err := service.NewService(
			service.GenericService(&service.GenericServiceDetails{
				Name:      endpointName,
				Namespace: instance.Namespace,
				Labels:    exportLabels,
				Selector:  serviceLabels,
				Port: service.GenericServicePort{
					Name:     endpointName,
					Port:     data.Port,
					Protocol: corev1.ProtocolTCP,
				},
			}),
			5,
			&svcOverride.OverrideSpec,
		)
		if err != nil {
			instance.Status.Conditions.Set(condition.FalseCondition(
				condition.ExposeServiceReadyCondition,
				condition.ErrorReason,
				condition.SeverityWarning,
				condition.ExposeServiceReadyErrorMessage,
				err.Error()))

			return ctrl.Result{}, err
		}

		svc.AddAnnotation(map[string]string{
			service.AnnotationEndpointKey: endpointTypeStr,
		})

		// add Annotation to whether creating an ingress is required or not
		if endpointType == service.EndpointPublic && svc.GetServiceType() == corev1.ServiceTypeClusterIP {
			svc.AddAnnotation(map[string]string{
				service.AnnotationIngressCreateKey: "true",
			})
		} else {
			svc.AddAnnotation(map[string]string{
				service.AnnotationIngressCreateKey: "false",
			})
			if svc.GetServiceType() == corev1.ServiceTypeLoadBalancer {
				svc.AddAnnotation(map[string]string{
					service.AnnotationHostnameKey: svc.GetServiceHostname(), // add annotation to register service name in dnsmasq
				})
			}
		}

		ctrlResult, err := svc.CreateOrPatch(ctx, helper)
		if err != nil {
			instance.Status.Conditions.Set(condition.FalseCondition(
				condition.ExposeServiceReadyCondition,
				condition.ErrorReason,
				condition.SeverityWarning,
				condition.ExposeServiceReadyErrorMessage,
				err.Error()))

			return ctrlResult, err
		} else if (ctrlResult != ctrl.Result{}) {
			instance.Status.Conditions.Set(condition.FalseCondition(
				condition.ExposeServiceReadyCondition,
				condition.RequestedReason,
				condition.SeverityInfo,
				condition.ExposeServiceReadyRunningMessage))
			return ctrlResult, nil
		}
		// create service - end

		// if TLS is enabled
		if instance.Spec.TLS.API.Enabled(endpointType) {
			// set endpoint protocol to https
			data.Protocol = ptr.To(service.ProtocolHTTPS)
		}

		apiEndpoints[string(endpointType)], err = svc.GetAPIEndpoint(
			svcOverride.EndpointURL, data.Protocol, data.Path)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	instance.Status.Conditions.MarkTrue(condition.ExposeServiceReadyCondition, condition.ExposeServiceReadyMessage)

	//
	// Update instance status with service endpoint url from route host information
	//
	instance.Status.APIEndpoints = apiEndpoints

	// expose service - end

	//
	// BootStrap Job
	//
	jobDef = keystone.BootstrapJob(instance, serviceLabels, serviceAnnotations, instance.Status.APIEndpoints)
	bootstrapjob := job.NewJob(
		jobDef,
		keystonev1.BootstrapHash,
		instance.Spec.PreserveJobs,
		5*time.Second,
		instance.Status.Hash[keystonev1.BootstrapHash],
	)
	ctrlResult, err = bootstrapjob.DoJob(
		ctx,
		helper,
	)
	if (ctrlResult != ctrl.Result{}) {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.BootstrapReadyCondition,
			condition.RequestedReason,
			condition.SeverityInfo,
			condition.BootstrapReadyRunningMessage))
		return ctrlResult, nil
	}
	if err != nil {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.BootstrapReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			condition.BootstrapReadyErrorMessage,
			err.Error()))
		return ctrl.Result{}, err
	}
	if bootstrapjob.HasChanged() {
		instance.Status.Hash[keystonev1.BootstrapHash] = bootstrapjob.GetHash()
		l.Info(fmt.Sprintf("Job %s hash added - %s", jobDef.Name, instance.Status.Hash[keystonev1.BootstrapHash]))
	}
	instance.Status.Conditions.MarkTrue(condition.BootstrapReadyCondition, condition.BootstrapReadyMessage)

	// run keystone bootstrap - end

	l.Info("Reconciled Service init successfully")
	return ctrl.Result{}, nil
}

func (r *KeystoneAPIReconciler) reconcileUpdate(ctx context.Context, instance *keystonev1.KeystoneAPI, helper *helper.Helper) (ctrl.Result, error) {
	l := GetLog(ctx)
	l.Info("Reconciling Service update")

	// TODO: should have minor update tasks if required
	// - delete dbsync hash from status to rerun it?

	l.Info("Reconciled Service update successfully")
	return ctrl.Result{}, nil
}

func (r *KeystoneAPIReconciler) reconcileUpgrade(ctx context.Context, instance *keystonev1.KeystoneAPI, helper *helper.Helper) (ctrl.Result, error) {
	l := GetLog(ctx)
	l.Info("Reconciling Service upgrade")

	// TODO: should have major version upgrade tasks
	// -delete dbsync hash from status to rerun it?

	l.Info("Reconciled Service upgrade successfully")
	return ctrl.Result{}, nil
}

func (r *KeystoneAPIReconciler) reconcileNormal(
	ctx context.Context,
	instance *keystonev1.KeystoneAPI,
	helper *helper.Helper,
) (ctrl.Result, error) {
	l := GetLog(ctx)
	l.Info("Reconciling Service")

	serviceLabels := map[string]string{
		common.AppSelector:   keystone.ServiceName,
		common.OwnerSelector: instance.Name,
	}

	// ConfigMap
	configMapVars := make(map[string]env.Setter)

	//
	// check for required OpenStack secret holding passwords for service/admin user and add hash to the vars map
	//
	ospSecret, hash, err := oko_secret.GetSecret(ctx, helper, instance.Spec.Secret, instance.Namespace)
	if err != nil {
		if k8s_errors.IsNotFound(err) {
			instance.Status.Conditions.Set(condition.FalseCondition(
				condition.InputReadyCondition,
				condition.RequestedReason,
				condition.SeverityInfo,
				condition.InputReadyWaitingMessage))
			return ctrl.Result{RequeueAfter: time.Second * 10}, fmt.Errorf("OpenStack secret %s not found", instance.Spec.Secret)
		}
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.InputReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			condition.InputReadyErrorMessage,
			err.Error()))
		return ctrl.Result{}, err
	}
	configMapVars[ospSecret.Name] = env.SetValue(hash)

	instance.Status.Conditions.MarkTrue(condition.InputReadyCondition, condition.InputReadyMessage)

	// run check OpenStack secret - end

	//
	// create RabbitMQ transportURL CR and get the actual URL from the associated secret that is created
	//
	transportURL, op, err := r.transportURLCreateOrUpdate(ctx, instance, serviceLabels)

	if err != nil {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.RabbitMqTransportURLReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			condition.RabbitMqTransportURLReadyErrorMessage,
			err.Error()))
		return ctrl.Result{}, err
	}
	if op != controllerutil.OperationResultNone {
		l.Info(fmt.Sprintf("TransportURL %s successfully reconciled - operation: %s", transportURL.Name, string(op)))
	}

	instance.Status.TransportURLSecret = transportURL.Status.SecretName

	if instance.Status.TransportURLSecret == "" {
		l.Info(fmt.Sprintf("Waiting for TransportURL %s secret to be created", transportURL.Name))
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.RabbitMqTransportURLReadyCondition,
			condition.RequestedReason,
			condition.SeverityInfo,
			condition.RabbitMqTransportURLReadyRunningMessage))
		return ctrl.Result{RequeueAfter: time.Duration(10) * time.Second}, nil
	}
	l.Info(fmt.Sprintf("TransportURL secret name %s", transportURL.Status.SecretName))
	instance.Status.Conditions.MarkTrue(condition.RabbitMqTransportURLReadyCondition, condition.RabbitMqTransportURLReadyMessage)
	// run check rabbitmq - end

	//
	// Check for required memcached used for caching
	//
	memcached, err := r.getKeystoneMemcached(ctx, helper, instance)
	if err != nil {
		if k8s_errors.IsNotFound(err) {
			instance.Status.Conditions.Set(condition.FalseCondition(
				condition.MemcachedReadyCondition,
				condition.RequestedReason,
				condition.SeverityInfo,
				condition.MemcachedReadyWaitingMessage))
			return ctrl.Result{RequeueAfter: 10 * time.Second}, fmt.Errorf("memcached %s not found", instance.Spec.MemcachedInstance)
		}
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.MemcachedReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			condition.MemcachedReadyErrorMessage,
			err.Error()))
		return ctrl.Result{}, err
	}

	// Add finalizer to Memcached to prevent it from being deleted now that we're using it
	if controllerutil.AddFinalizer(memcached, helper.GetFinalizer()) {
		err := r.Update(ctx, memcached)

		if err != nil {
			instance.Status.Conditions.Set(condition.FalseCondition(
				condition.MemcachedReadyCondition,
				condition.ErrorReason,
				condition.SeverityWarning,
				condition.MemcachedReadyErrorMessage,
				err.Error()))
			return ctrl.Result{}, err
		}
	}

	if !memcached.IsReady() {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.MemcachedReadyCondition,
			condition.RequestedReason,
			condition.SeverityInfo,
			condition.MemcachedReadyWaitingMessage))
		return ctrl.Result{RequeueAfter: 10 * time.Second}, fmt.Errorf("memcached %s is not ready", memcached.Name)
	}
	// Mark the Memcached Service as Ready if we get to this point with no errors
	instance.Status.Conditions.MarkTrue(
		condition.MemcachedReadyCondition, condition.MemcachedReadyMessage)
	// run check memcached - end

	//
	// Create ConfigMaps and Secrets required as input for the Service and calculate an overall hash of hashes
	//

	//
	// create Configmap required for keystone input
	// - %-scripts configmap holding scripts to e.g. bootstrap the service
	// - %-config configmap holding minimal keystone config required to get the service up, user can add additional files to be added to the service
	// - parameters which has passwords gets added from the OpenStack secret via the init container
	//
	err = r.generateServiceConfigMaps(ctx, instance, helper, &configMapVars, memcached)
	if err != nil {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.ServiceConfigReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			condition.ServiceConfigReadyErrorMessage,
			err.Error()))
		return ctrl.Result{}, err
	}

	//
	// Create secret holding fernet keys (for token and credential)
	//
	// TODO key rotation
	err = r.ensureFernetKeys(ctx, instance, helper, &configMapVars)
	if err != nil {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.ServiceConfigReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			condition.ServiceConfigReadyErrorMessage,
			err.Error()))
		return ctrl.Result{}, err
	}

	//
	// TLS input validation
	//
	// Validate the CA cert secret if provided
	if instance.Spec.TLS.CaBundleSecretName != "" {
		hash, ctrlResult, err := tls.ValidateCACertSecret(
			ctx,
			helper.GetClient(),
			types.NamespacedName{
				Name:      instance.Spec.TLS.CaBundleSecretName,
				Namespace: instance.Namespace,
			},
		)
		if err != nil {
			instance.Status.Conditions.Set(condition.FalseCondition(
				condition.TLSInputReadyCondition,
				condition.ErrorReason,
				condition.SeverityWarning,
				condition.TLSInputErrorMessage,
				err.Error()))
			return ctrlResult, err
		} else if (ctrlResult != ctrl.Result{}) {
			return ctrlResult, nil
		}

		if hash != "" {
			configMapVars[tls.CABundleKey] = env.SetValue(hash)
		}
	}

	// Validate API service certs secrets
	certsHash, ctrlResult, err := instance.Spec.TLS.API.ValidateCertSecrets(ctx, helper, instance.Namespace)
	if err != nil {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.TLSInputReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			condition.TLSInputErrorMessage,
			err.Error()))
		return ctrlResult, err
	} else if (ctrlResult != ctrl.Result{}) {
		return ctrlResult, nil
	}
	configMapVars[tls.TLSHashName] = env.SetValue(certsHash)

	// all cert input checks out so report InputReady
	instance.Status.Conditions.MarkTrue(condition.TLSInputReadyCondition, condition.InputReadyMessage)

	// create hash over all the different input resources to identify if any those changed
	// and a restart/recreate is required.
	inputHash, hashChanged, err := r.createHashOfInputHashes(ctx, instance, configMapVars)
	if err != nil {
		return ctrl.Result{}, err
	} else if hashChanged {
		// Hash changed and instance status should be updated (which will be done by main defer func),
		// so we need to return and reconcile again
		return ctrl.Result{}, nil
	}
	instance.Status.Conditions.MarkTrue(condition.ServiceConfigReadyCondition, condition.ServiceConfigReadyMessage)

	// Create ConfigMaps and Secrets - end

	//
	// TODO check when/if Init, Update, or Upgrade should/could be skipped
	//

	// networks to attach to
	for _, netAtt := range instance.Spec.NetworkAttachments {
		_, err := nad.GetNADWithName(ctx, helper, netAtt, instance.Namespace)
		if err != nil {
			if k8s_errors.IsNotFound(err) {
				instance.Status.Conditions.Set(condition.FalseCondition(
					condition.NetworkAttachmentsReadyCondition,
					condition.RequestedReason,
					condition.SeverityInfo,
					condition.NetworkAttachmentsReadyWaitingMessage,
					netAtt))
				return ctrl.Result{RequeueAfter: time.Second * 10}, fmt.Errorf("network-attachment-definition %s not found", netAtt)
			}
			instance.Status.Conditions.Set(condition.FalseCondition(
				condition.NetworkAttachmentsReadyCondition,
				condition.ErrorReason,
				condition.SeverityWarning,
				condition.NetworkAttachmentsReadyErrorMessage,
				err.Error()))
			return ctrl.Result{}, err
		}
	}

	serviceAnnotations, err := nad.CreateNetworksAnnotation(instance.Namespace, instance.Spec.NetworkAttachments)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed create network annotation from %s: %w",
			instance.Spec.NetworkAttachments, err)
	}

	// Handle service init
	ctrlResult, err = r.reconcileInit(ctx, instance, helper, serviceLabels, serviceAnnotations)
	if err != nil {
		return ctrlResult, err
	} else if (ctrlResult != ctrl.Result{}) {
		return ctrlResult, nil
	}

	// Handle service update
	ctrlResult, err = r.reconcileUpdate(ctx, instance, helper)
	if err != nil {
		return ctrlResult, err
	} else if (ctrlResult != ctrl.Result{}) {
		return ctrlResult, nil
	}

	// Handle service upgrade
	ctrlResult, err = r.reconcileUpgrade(ctx, instance, helper)
	if err != nil {
		return ctrlResult, err
	} else if (ctrlResult != ctrl.Result{}) {
		return ctrlResult, nil
	}

	//
	// normal reconcile tasks
	//

	// Define a new Deployment object
	deplDef, err := keystone.Deployment(ctx, helper, instance, inputHash, serviceLabels, serviceAnnotations)
	if err != nil {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.DeploymentReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			condition.DeploymentReadyErrorMessage,
			err.Error()))
		return ctrl.Result{}, err
	}
	depl := deployment.NewDeployment(
		deplDef,
		5*time.Second,
	)

	ctrlResult, err = depl.CreateOrPatch(ctx, helper)
	if err != nil {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.DeploymentReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			condition.DeploymentReadyErrorMessage,
			err.Error()))
		return ctrlResult, err
	} else if (ctrlResult != ctrl.Result{}) {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.DeploymentReadyCondition,
			condition.RequestedReason,
			condition.SeverityInfo,
			condition.DeploymentReadyRunningMessage))
		return ctrlResult, nil
	}
	instance.Status.ReadyCount = depl.GetDeployment().Status.ReadyReplicas

	// verify if network attachment matches expectations
	networkReady, networkAttachmentStatus, err := nad.VerifyNetworkStatusFromAnnotation(ctx, helper, instance.Spec.NetworkAttachments, serviceLabels, instance.Status.ReadyCount)
	if err != nil {
		return ctrl.Result{}, err
	}

	instance.Status.NetworkAttachments = networkAttachmentStatus
	if networkReady {
		instance.Status.Conditions.MarkTrue(condition.NetworkAttachmentsReadyCondition, condition.NetworkAttachmentsReadyMessage)
	} else {
		err := fmt.Errorf("not all pods have interfaces with ips as configured in NetworkAttachments: %s", instance.Spec.NetworkAttachments)
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.NetworkAttachmentsReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			condition.NetworkAttachmentsReadyErrorMessage,
			err.Error()))

		return ctrl.Result{}, err
	}

	if instance.Status.ReadyCount > 0 {
		instance.Status.Conditions.MarkTrue(condition.DeploymentReadyCondition, condition.DeploymentReadyMessage)
	} else {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.DeploymentReadyCondition,
			condition.RequestedReason,
			condition.SeverityInfo,
			condition.DeploymentReadyRunningMessage))
	}
	// create Deployment - end

	// create CronJob
	cronjobDef := keystone.CronJob(instance, serviceLabels, serviceAnnotations)
	cronjob := cronjob.NewCronJob(
		cronjobDef,
		5*time.Second,
	)

	ctrlResult, err = cronjob.CreateOrPatch(ctx, helper)
	if err != nil {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.CronJobReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			condition.CronJobReadyErrorMessage,
			err.Error()))
		return ctrlResult, err
	}

	instance.Status.Conditions.MarkTrue(condition.CronJobReadyCondition, condition.CronJobReadyMessage)
	// create CronJob - end

	//
	// create OpenStackClient config
	//
	err = r.reconcileCloudConfig(ctx, helper, instance)
	if err != nil {
		return ctrl.Result{}, err
	}

	l.Info("Reconciled Service successfully")
	return ctrl.Result{}, nil
}

func (r *KeystoneAPIReconciler) transportURLCreateOrUpdate(
	ctx context.Context,
	instance *keystonev1.KeystoneAPI,
	serviceLabels map[string]string,
) (*rabbitmqv1.TransportURL, controllerutil.OperationResult, error) {
	transportURL := &rabbitmqv1.TransportURL{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-keystone-transport", instance.Name),
			Namespace: instance.Namespace,
			Labels:    serviceLabels,
		},
	}
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, transportURL, func() error {
		transportURL.Spec.RabbitmqClusterName = instance.Spec.RabbitMqClusterName
		err := controllerutil.SetControllerReference(instance, transportURL, r.Scheme)
		return err
	})

	return transportURL, op, err
}

// generateServiceConfigMaps - create create configmaps which hold scripts and service configuration
// TODO add DefaultConfigOverwrite
func (r *KeystoneAPIReconciler) generateServiceConfigMaps(
	ctx context.Context,
	instance *keystonev1.KeystoneAPI,
	h *helper.Helper,
	envVars *map[string]env.Setter,
	mc *memcachedv1.Memcached,
) error {
	//
	// create Configmap/Secret required for keystone input
	// - %-scripts configmap holding scripts to e.g. bootstrap the service
	// - %-config configmap holding minimal keystone config required to get the service up, user can add additional files to be added to the service
	// - parameters which has passwords gets added from the ospSecret via the init container
	//

	cmLabels := labels.GetLabels(instance, labels.GetGroupLabel(keystone.ServiceName), map[string]string{})

	// customData hold any customization for the service.
	// custom.conf is going to /etc/<service>/<service>.conf.d
	// all other files get placed into /etc/<service> to allow overwrite of e.g. policy.json
	// TODO: make sure custom.conf can not be overwritten
	customData := map[string]string{common.CustomServiceConfigFileName: instance.Spec.CustomServiceConfig}
	for key, data := range instance.Spec.DefaultConfigOverwrite {
		customData[key] = data
	}

	transportURLSecret, _, err := secret.GetSecret(ctx, h, instance.Status.TransportURLSecret, instance.Namespace)
	if err != nil {
		return err
	}

	ospSecret, _, err := secret.GetSecret(ctx, h, instance.Spec.Secret, instance.Namespace)
	if err != nil {
		return err
	}

	templateParameters := map[string]interface{}{
		"memcachedServers": strings.Join(mc.Status.ServerList, ","),
		"TransportURL":     string(transportURLSecret.Data["transport_url"]),
		"DatabaseConnection": fmt.Sprintf("mysql+pymysql://%s:%s@%s/%s",
			instance.Spec.DatabaseUser,
			string(ospSecret.Data[instance.Spec.PasswordSelectors.Database]),
			instance.Status.DatabaseHostname,
			keystone.DatabaseName,
		),
	}

	// create httpd  vhost template parameters
	httpdVhostConfig := map[string]interface{}{}
	for _, endpt := range []service.Endpoint{service.EndpointInternal, service.EndpointPublic} {
		endptConfig := map[string]interface{}{}
		endptConfig["ServerName"] = fmt.Sprintf("%s-%s.%s.svc", instance.Name, endpt.String(), instance.Namespace)
		endptConfig["TLS"] = false // default TLS to false, and set it bellow to true if enabled
		if instance.Spec.TLS.API.Enabled(endpt) {
			endptConfig["TLS"] = true
			endptConfig["SSLCertificateFile"] = fmt.Sprintf("/etc/pki/tls/certs/%s.crt", endpt.String())
			endptConfig["SSLCertificateKeyFile"] = fmt.Sprintf("/etc/pki/tls/private/%s.key", endpt.String())
		}
		httpdVhostConfig[endpt.String()] = endptConfig
	}
	templateParameters["VHosts"] = httpdVhostConfig

	tmpl := []util.Template{
		// Scripts
		{
			Name:         fmt.Sprintf("%s-scripts", instance.Name),
			Namespace:    instance.Namespace,
			Type:         util.TemplateTypeScripts,
			InstanceType: instance.Kind,
			Labels:       cmLabels,
		},
		// Configs
		{
			Name:          fmt.Sprintf("%s-config-data", instance.Name),
			Namespace:     instance.Namespace,
			Type:          util.TemplateTypeConfig,
			InstanceType:  instance.Kind,
			CustomData:    customData,
			ConfigOptions: templateParameters,
			Labels:        cmLabels,
		},
	}
	return secret.EnsureSecrets(ctx, h, instance, tmpl, envVars)
}

// reconcileConfigMap -  creates clouds.yaml
// TODO: most likely should be part of the higher openstack operator
func (r *KeystoneAPIReconciler) reconcileCloudConfig(
	ctx context.Context,
	h *helper.Helper,
	instance *keystonev1.KeystoneAPI) error {
	// clouds.yaml
	var openStackConfig keystone.OpenStackConfig
	templateParameters := make(map[string]interface{})
	cmLabels := labels.GetLabels(instance, labels.GetGroupLabel(keystone.ServiceName), map[string]string{})

	authURL, err := instance.GetEndpoint(endpoint.EndpointPublic)
	if err != nil {
		return err
	}
	openStackConfig.Clouds.Default.Auth.AuthURL = authURL
	openStackConfig.Clouds.Default.Auth.ProjectName = instance.Spec.AdminProject
	openStackConfig.Clouds.Default.Auth.UserName = instance.Spec.AdminUser
	openStackConfig.Clouds.Default.Auth.UserDomainName = "Default"
	openStackConfig.Clouds.Default.Auth.ProjectDomainName = "Default"
	openStackConfig.Clouds.Default.RegionName = instance.Spec.Region

	cloudsYamlVal, err := yaml.Marshal(&openStackConfig)
	if err != nil {
		return err
	}
	cloudsYaml := map[string]string{
		"clouds.yaml": string(cloudsYamlVal),
		"OS_CLOUD":    "default",
	}

	cms := []util.Template{
		{
			Name:          "openstack-config",
			Namespace:     instance.Namespace,
			Type:          util.TemplateTypeNone,
			InstanceType:  instance.Kind,
			CustomData:    cloudsYaml,
			ConfigOptions: templateParameters,
			Labels:        cmLabels,
		},
	}
	err = configmap.EnsureConfigMaps(ctx, h, instance, cms, nil)
	if err != nil {
		return err
	}

	// secure.yaml
	keystoneSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Spec.Secret,
			Namespace: instance.Namespace,
		},
		Type: "Opaque",
	}

	err = r.Client.Get(ctx, types.NamespacedName{Name: keystoneSecret.Name, Namespace: instance.Namespace}, keystoneSecret)
	if err != nil {
		return err
	}

	var openStackConfigSecret keystone.OpenStackConfigSecret
	openStackConfigSecret.Clouds.Default.Auth.Password = string(keystoneSecret.Data[instance.Spec.PasswordSelectors.Admin])

	secretVal, err := yaml.Marshal(&openStackConfigSecret)
	if err != nil {
		return err
	}
	secretString := map[string]string{
		"secure.yaml": string(secretVal),
	}

	secrets := []util.Template{
		{
			Name:          "openstack-config-secret",
			Namespace:     instance.Namespace,
			Type:          util.TemplateTypeNone,
			InstanceType:  instance.Kind,
			CustomData:    secretString,
			ConfigOptions: templateParameters,
			Labels:        cmLabels,
		},
	}

	return oko_secret.EnsureSecrets(ctx, h, instance, secrets, nil)
}

// ensureFernetKeys - creates secret with fernet keys
func (r *KeystoneAPIReconciler) ensureFernetKeys(
	ctx context.Context,
	instance *keystonev1.KeystoneAPI,
	helper *helper.Helper,
	envVars *map[string]env.Setter,
) error {
	labels := labels.GetLabels(instance, labels.GetGroupLabel(keystone.ServiceName), map[string]string{})

	//
	// check if secret already exist
	//
	secretName := keystone.ServiceName
	secret, hash, err := oko_secret.GetSecret(ctx, helper, secretName, instance.Namespace)
	if err != nil && !k8s_errors.IsNotFound(err) {
		return err
	} else if k8s_errors.IsNotFound(err) {
		fernetKeys := map[string]string{
			"FernetKeys0":     keystone.GenerateFernetKey(),
			"FernetKeys1":     keystone.GenerateFernetKey(),
			"CredentialKeys0": keystone.GenerateFernetKey(),
			"CredentialKeys1": keystone.GenerateFernetKey(),
		}

		tmpl := []util.Template{
			{
				Name:       secretName,
				Namespace:  instance.Namespace,
				Type:       util.TemplateTypeNone,
				CustomData: fernetKeys,
				Labels:     labels,
			},
		}
		err := oko_secret.EnsureSecrets(ctx, helper, instance, tmpl, envVars)

		if err != nil {
			return err
		}
	} else {
		// add hash to envVars
		(*envVars)[secret.Name] = env.SetValue(hash)
	}

	// TODO: fernet key rotation

	return nil
}

// createHashOfInputHashes - creates a hash of hashes which gets added to the resources which requires a restart
// if any of the input resources change, like configs, passwords, ...
//
// returns the hash, whether the hash changed (as a bool) and any error
func (r *KeystoneAPIReconciler) createHashOfInputHashes(
	ctx context.Context,
	instance *keystonev1.KeystoneAPI,
	envVars map[string]env.Setter,
) (string, bool, error) {
	var hashMap map[string]string
	changed := false
	mergedMapVars := env.MergeEnvs([]corev1.EnvVar{}, envVars)
	hash, err := util.ObjectHash(mergedMapVars)
	if err != nil {
		return hash, changed, err
	}
	if hashMap, changed = util.SetHash(instance.Status.Hash, common.InputHashName, hash); changed {
		instance.Status.Hash = hashMap
		GetLog(ctx).Info(fmt.Sprintf("Input maps hash %s - %s", common.InputHashName, hash))
	}
	return hash, changed, nil
}

// getKeystoneMemcached - gets the Memcached instance used for keystone cache backend
func (r *KeystoneAPIReconciler) getKeystoneMemcached(
	ctx context.Context,
	h *helper.Helper,
	instance *keystonev1.KeystoneAPI,
) (*memcachedv1.Memcached, error) {
	memcached := &memcachedv1.Memcached{}
	err := h.GetClient().Get(
		ctx,
		types.NamespacedName{
			Name:      instance.Spec.MemcachedInstance,
			Namespace: instance.Namespace,
		},
		memcached)
	if err != nil {
		return nil, err
	}
	return memcached, err
}
