# K8s config reloader 

Automatically trigger a new rollout for `Deployment`, `StatefulSet` and `DaemonSet`
upon ConfigMap or Secret changes.
Usage
1. Label Secret or ConfigMap with label `label-foo: bar`
2. Label K8s Workload with label `label-foo: bar` 

K8s config reloader watch for all CM/Secrets, upon a change, it will search and 
issue a new rollout for a workload.  

Default match label: `mlops.cnvrg.io`, to change default match label, 
set `--match-lable foo-bar`

Examples:

If following CM given  
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  labels:
    cnvrg-config-reloader.mlops.cnvrg.io: "autoreload-ccp"
```

And following deployments defined
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app1
  labels:
    cnvrg-config-reloader.mlops.cnvrg.io: "autoreload-ccp"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app2
  labels:
    cnvrg-config-reloader.mlops.cnvrg.io: "autoreload-ccp"
---
```

And `--match-lable=cnvrg-config-reloader.mlops.cnvrg.io`, 
K8s config reloader, will issue rollout to all pods that's 
belongs to `app1` and `app2` deployments.