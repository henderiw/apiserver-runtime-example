package target

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	invv1alpha1 "github.com/henderiw/apiserver-runtime-example/apis/inv/v1alpha1"
	dsclient "github.com/henderiw/apiserver-runtime-example/pkg/dataserver/client"
	"github.com/henderiw/apiserver-runtime-example/pkg/reconcilers"
	"github.com/henderiw/apiserver-runtime-example/pkg/reconcilers/context/dsctx"
	"github.com/henderiw/apiserver-runtime-example/pkg/reconcilers/ctrlconfig"
	"github.com/henderiw/apiserver-runtime-example/pkg/reconcilers/resource"
	"github.com/henderiw/apiserver-runtime-example/pkg/store"
	"github.com/henderiw/apiserver-runtime-example/pkg/target"
	sdcpb "github.com/iptecharch/sdc-protos/sdcpb"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/prototext"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

func init() {
	reconcilers.Register("target", &reconciler{})
}

const (
	finalizer = "infra.nephio.org/finalizer"
	// errors
	errGetCr           = "cannot get cr"
	errUpdateDataStore = "cannot update datastore"
)

type adder interface {
	Add(item interface{})
}

//+kubebuilder:rbac:groups=inv.nephio.org,resources=targets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=inv.nephio.org,resources=targets/status,verbs=get;update;patch

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {
	cfg, ok := c.(*ctrlconfig.ControllerConfig)
	if !ok {
		return nil, fmt.Errorf("cannot initialize, expecting controllerConfig, got: %s", reflect.TypeOf(c).Name())
	}

	if err := invv1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		return nil, err
	}

	r.Client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer)
	r.configStore = cfg.ConfigStore
	r.targetStore = cfg.TargetStore
	r.dataServerStore = cfg.DataServerStore

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named("TargetDataStoreController").
		For(&invv1alpha1.Target{}).
		Watches(&source.Kind{Type: &invv1alpha1.TargetConnectionProfile{}}, &targetConnProfileEventHandler{client: mgr.GetClient()}).
		Watches(&source.Kind{Type: &invv1alpha1.TargetSyncProfile{}}, &targetSyncProfileEventHandler{client: mgr.GetClient()}).
		Complete(r)
}

type reconciler struct {
	client.Client
	finalizer *resource.APIFinalizer

	configStore     store.Storer[runtime.Object]
	targetStore     store.Storer[target.Context]
	dataServerStore store.Storer[dsctx.Context]
}

// logic
/*
target get deleted
1. delete the datastore from the dataserver
2. delete the finalizer

target get created or updated:
1. add a finalizer
2. update target cache and assign a dataserver for this target
3. create or update(delete/create) the datastore if there are changes
*/

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("req", req)
	log.Info("reconcile")

	key := store.GetNSNKey(req.NamespacedName)

	cr := &invv1alpha1.Target{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if resource.IgnoreNotFound(err) != nil {
			log.Error(err, errGetCr)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}

	if !cr.GetDeletionTimestamp().IsZero() {
		// check if this is the last one -> if so stop the client to the dataserver
		currentTargetCtx, err := r.targetStore.Get(ctx, key)
		if err != nil {
			// client does not exist
			if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
				log.Error(err, "cannot remove finalizer")
				return ctrl.Result{Requeue: true}, err
			}
			log.Info("Successfully deleted resource, with non existing client -> strange")
			return ctrl.Result{}, nil
		}
		// delete the mapping in the dataserver cache, which keeps track of all targets per dataserver
		r.deleteTargetFromDataServer(ctx, store.GetNameKey(currentTargetCtx.Client.GetAddress()), key)
		// delete the datastore
		if currentTargetCtx.DataStore != nil {
			rsp, err := currentTargetCtx.Client.DeleteDataStore(ctx, &sdcpb.DeleteDataStoreRequest{Name: key.String()})
			if err != nil {
				log.Error(err, "cannot delete datastore")
				return ctrl.Result{Requeue: true}, err
			}
			log.Info("delete datastore succeeded", "resp", prototext.Format(rsp))
		}

		// delete the target from the target store
		r.targetStore.Delete(ctx, store.GetNSNKey(req.NamespacedName))
		// remove the finalizer
		if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
			log.Error(err, "cannot remove finalizer")
			return ctrl.Result{Requeue: true}, err
		}

		log.Info("Successfully deleted resource")
		return ctrl.Result{}, nil
	}
	if err := r.finalizer.AddFinalizer(ctx, cr); err != nil {
		log.Error(err, "cannot add finalizer")
		return ctrl.Result{}, err
	}

	// first check if the target has an assigned dataserver, if not allocate one
	// update the target store with the updated information
	currentTargetCtx, err := r.targetStore.Get(ctx, store.GetNSNKey(req.NamespacedName))
	if err != nil {
		// target does not exist -> select a dataServer
		selectedDSctx, err := r.selectDataServerContext(ctx)
		if err != nil {
			log.Error(err, "cannot select a dataserver")
			return ctrl.Result{}, err
		}
		// add the target to the DS
		r.addTargetToDataServer(ctx, store.GetNameKey(selectedDSctx.Client.GetAddress()), key)
		// create the target in the target store
		r.targetStore.Create(ctx, store.GetNSNKey(req.NamespacedName), target.Context{
			Client: selectedDSctx.Client,
		})
	} else {
		// safety
		r.addTargetToDataServer(ctx, store.GetNameKey(currentTargetCtx.Client.GetAddress()), key)
	}
	// Now that the target store is up to date and we have an assigned dataserver
	// we will create/update the datastore for the target
	return ctrl.Result{}, errors.Wrap(r.updateDataStore(ctx, cr), errUpdateDataStore)
}

func (r *reconciler) deleteTargetFromDataServer(ctx context.Context, dsKey store.Key, targetKey store.Key) {
	log := log.FromContext(ctx)
	dsctx, err := r.dataServerStore.Get(ctx, dsKey)
	if err != nil {
		log.Info("DeleteTarget2DataServer dataserver key not found", "dsKey", dsKey, "targetKey", targetKey, "error", err.Error())
		return
	}
	dsctx.Targets = dsctx.Targets.Delete(targetKey.String())
	if err := r.dataServerStore.Update(ctx, dsKey, dsctx); err != nil {
		log.Info("DeleteTarget2DataServer dataserver update failed", "dsKey", dsKey, "targetKey", targetKey, "error", err.Error())
	}
}

func (r *reconciler) addTargetToDataServer(ctx context.Context, dsKey store.Key, targetKey store.Key) {
	log := log.FromContext(ctx)
	dsctx, err := r.dataServerStore.Get(ctx, dsKey)
	if err != nil {
		log.Info("AddTarget2DataServer dataserver key not found", "dsKey", dsKey, "targetKey", targetKey, "error", err.Error())
		return
	}
	dsctx.Targets = dsctx.Targets.Insert(targetKey.String())
	if err := r.dataServerStore.Update(ctx, dsKey, dsctx); err != nil {
		log.Info("AddTarget2DataServer dataserver update failed", "dsKey", dsKey, "targetKey", targetKey, "error", err.Error())
	}
}

// selectDataServerContext selects a dataserver for a particalur target based on
// least amount of targets assigned to a dataserver
func (r *reconciler) selectDataServerContext(ctx context.Context) (*dsctx.Context, error) {
	log := log.FromContext(ctx)
	var err error
	var selectedDSctx *dsctx.Context
	minTargets := -1
	r.dataServerStore.List(ctx, func(ctx context.Context, k store.Key, dsctx dsctx.Context) {
		if dsctx.Targets.Len() == 0 || dsctx.Targets.Len() < minTargets {
			selectedDSctx = &dsctx
			minTargets = dsctx.Targets.Len()
		}
	})
	// create and start client if it does not exist
	if selectedDSctx.Client == nil {
		selectedDSctx.Client, err = dsclient.New(selectedDSctx.Config)
		if err != nil {
			// happens when address or config is not set properly
			log.Error(err, "cannot create dataserver client")
			return nil, err
		}
		if err := selectedDSctx.Client.Start(ctx); err != nil {
			log.Error(err, "cannot start dataserver client")
			return nil, err
		}
	}
	return selectedDSctx, nil
}

// updateDataStore will do 1 of 3 things
// 1. create a datastore if none exists
// 2. delete/update the datastore if changes were detected
// 3. do nothing if no changes were detected.
func (r *reconciler) updateDataStore(ctx context.Context, cr *invv1alpha1.Target) error {
	key := store.GetNSNKey(types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.GetName()})
	log := log.FromContext(ctx).WithValues("targetkey", key.String())

	req, err := r.getCreateDataStoreRequest(ctx, cr)
	if err != nil {
		log.Error(err, "cannot create datastore request from CR/Profiles")
		return err
	}
	// this should always succeed
	targetCtx, err := r.targetStore.Get(ctx, key)
	if err != nil {
		log.Error(err, "cannot get datastore from store")
		return err
	}
	// get the datastore from the dataserver
	getRsp, err := targetCtx.Client.GetDataStore(ctx, &sdcpb.GetDataStoreRequest{Name: key.String()})
	if err != nil {
		if !strings.Contains(err.Error(), "unknown datastore") {
			log.Error(err, "cannot get datastore from dataserver")
			return err
		}
		log.Info("datastore does not exist")
		// datastore does not exist
	} else {

		// datastore exists -< validate changes and if so delete the datastore
		if r.validateDataStoreChanges(ctx, req, getRsp) {
			log.Info("datastore exist -> changed")
			rsp, err := targetCtx.Client.DeleteDataStore(ctx, &sdcpb.DeleteDataStoreRequest{Name: key.String()})
			if err != nil {
				log.Error(err, "cannot delete datstore in dataserver")
				return err
			}
			log.Info("delete datastore succeeded", "resp", prototext.Format(rsp))
		} else {
			log.Info("datastore exist -> no change")
			return nil
		}
	}
	// datastore does not exist -> create datastore
	rsp, err := targetCtx.Client.CreateDataStore(ctx, req)
	if err != nil {
		log.Error(err, "cannot create datastore in dataserver")
		return err
	}
	targetCtx.DataStore = req
	if err := r.targetStore.Update(ctx, key, targetCtx); err != nil {
		log.Error(err, "cannot update datastore in store")
		return err
	}
	log.Info("create datastore succeeded", "resp", prototext.Format(rsp))
	return nil
}

func (r *reconciler) validateDataStoreChanges(ctx context.Context, req *sdcpb.CreateDataStoreRequest, rsp *sdcpb.GetDataStoreResponse) bool {
	log := log.FromContext(ctx)
	//key := store.GetNSNKey(types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.GetName()})
	if req.Name != rsp.Name {
		return true
	}

	if req.Schema.Name != rsp.Schema.Name ||
		req.Schema.Vendor != rsp.Schema.Vendor ||
		req.Schema.Version != rsp.Schema.Version {
		return true
	}
	if req.Target.Type != rsp.Target.Type ||
		req.Target.Address != rsp.Target.Address {
		return true
	}

	log.Info("validateDataStoreChanges", "reqCreds", req.Target.Credentials, "rspCreds", rsp.Target.Credentials)
	if rsp.Target.Credentials == nil {
		return true
	} 
	if req.Target.Credentials.Username != rsp.Target.Credentials.Username ||
		req.Target.Credentials.Password != rsp.Target.Credentials.Password {
		return true
	}

	log.Info("validateDataStoreChanges", "reqTLS", req.Target.Tls, "rspTLS", rsp.Target.Tls)
	if req.Target.Tls.Ca != rsp.Target.Tls.Ca ||
		req.Target.Tls.Cert != rsp.Target.Tls.Cert ||
		req.Target.Tls.Key != rsp.Target.Tls.Key ||
		req.Target.Tls.SkipVerify != rsp.Target.Tls.SkipVerify {
		return true
	}
	return false
}

func (r *reconciler) getConnProfile(ctx context.Context, key types.NamespacedName) (*invv1alpha1.TargetConnectionProfile, error) {
	profile := &invv1alpha1.TargetConnectionProfile{}
	if err := r.Get(ctx, key, profile); err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return nil, err
		}
		return invv1alpha1.DefaultTargetConnectionProfile(), nil
	}
	return profile, nil
}

func (r *reconciler) getSyncProfile(ctx context.Context, key types.NamespacedName) (*invv1alpha1.TargetSyncProfile, error) {
	profile := &invv1alpha1.TargetSyncProfile{}
	if err := r.Get(ctx, key, profile); err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return nil, err
		}
		return invv1alpha1.DefaultTargetSyncProfile(), nil
	}
	return profile, nil
}

func (r *reconciler) getSecret(ctx context.Context, key types.NamespacedName) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, key, secret); err != nil {
		return nil, err
	}
	return secret, nil
}

func (r *reconciler) getCreateDataStoreRequest(ctx context.Context, cr *invv1alpha1.Target) (*sdcpb.CreateDataStoreRequest, error) {
	syncProfile, err := r.getSyncProfile(ctx, types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.Spec.SyncProfile})
	if err != nil {
		return nil, err
	}
	connProfile, err := r.getConnProfile(ctx, types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.Spec.ConnectionProfile})
	if err != nil {
		return nil, err
	}
	secret, err := r.getSecret(ctx, types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.Spec.SecretName})
	if err != nil {
		return nil, err
	}
	tls := &sdcpb.TLS{}
	if connProfile.Spec.SkipVerify != nil {
		tls.SkipVerify = *connProfile.Spec.SkipVerify
	}
	if cr.Spec.TLSSecretName != nil {
		tlsSecret, err := r.getSecret(ctx, types.NamespacedName{Namespace: cr.GetNamespace(), Name: *cr.Spec.TLSSecretName})
		if err != nil {
			return nil, err
		}
		tls.Ca = string(tlsSecret.Data["ca"])
		tls.Cert = string(tlsSecret.Data["cert"])
		tls.Key = string(tlsSecret.Data["key"])
	}

	return &sdcpb.CreateDataStoreRequest{
		Name: store.GetNSNKey(types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name}).String(),
		Target: &sdcpb.Target{
			Type:    string(connProfile.Spec.Protocol),
			Address: *cr.Spec.Address,
			Credentials: &sdcpb.Credentials{
				Username: string(secret.Data["username"]),
				Password: string(secret.Data["password"]),
			},
			Tls: tls,
		},
		Sync: invv1alpha1.GetSyncProfile(syncProfile),
		Schema: &sdcpb.Schema{
			Name:    cr.Spec.Provider.Name,    // TODO should come from the target profile
			Vendor:  cr.Spec.Provider.Vendor,  // TODO should come from the target profile
			Version: cr.Spec.Provider.Version, // TODO should come from the target profile
		},
	}, nil
}
