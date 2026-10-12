# RUNBOOK: go-k8s-todos

Prod runs in namespace `go-api`. ArgoCD app `go-k8s-todos` syncs `k8s/overlays/prod` from `main`.

## How a deploy works

1. Open a PR. `ci.yml` runs lint, tests, manifest validation and an image build. Merge once green (`main` is protected: PR required, CI must pass, no force pushes).
2. `cd.yml` runs tests, then builds and pushes `ghcr.io/abhinandkakkadi/go-k8s-todos:sha-<7>`.
3. `cd.yml` commits the new tag to `k8s/overlays/prod/kustomization.yaml`.
4. ArgoCD sees the commit and rolls out the Deployment.

## One-time setup

### Branch protection and the CD deploy key

`main` is protected by the ruleset `main-protection`: PR required, checks `lint`, `test`, `manifests`, `docker` must pass, no force push, no deletion.
The only bypass actor is the deploy key whose private half is the repo secret `DEPLOY_KEY`. `cd.yml` uses it to push the tag bump to `main`.

Rotate the key:

```sh
ssh-keygen -t ed25519 -N '' -C cd-bump-tag -f /tmp/cd-key
gh repo deploy-key add /tmp/cd-key.pub --allow-write --title cd-bump-tag
gh secret set DEPLOY_KEY < /tmp/cd-key
rm /tmp/cd-key /tmp/cd-key.pub
# Then delete the old key (gh repo deploy-key list / delete) and add the new key ID
# to the ruleset bypass list: Settings -> Rules -> main-protection.
```

### Secret (never committed)

`secret.yaml` is in `.gitignore`. Create the secret by hand in each cluster:

```sh
kubectl create namespace go-api --dry-run=client -o yaml | kubectl apply -f -
kubectl -n go-api create secret generic go-api-db \
  --from-literal=DB_USER='<user>' \
  --from-literal=DB_PASSWORD='<password>'
```

Pods will not start (`CreateContainerConfigError`) until this secret exists.

### Image pull access

GHCR packages are private by default. Pick one:
- Make the package public: GitHub → Packages → go-k8s-todos → Package settings → Change visibility.
- Or create a pull secret and add `imagePullSecrets` to the Deployment:
  ```sh
  kubectl -n go-api create secret docker-registry ghcr \
    --docker-server=ghcr.io --docker-username=<gh-user> --docker-password=<PAT with read:packages>
  ```

### ArgoCD

The project must exist before the Application that references it:

```sh
kubectl apply -f argocd/project.yaml -f argocd/application.yaml
```

ArgoCD does not watch `argocd/`. After changing either file, re-run the command above.

The project only allows the kinds listed in `argocd/project.yaml`. Adding a new kind under `k8s/` (e.g. a HorizontalPodAutoscaler) without adding it there fails the sync with `resource ... is not permitted in project`.

## Network policy

`k8s/base/networkpolicy.yaml` denies all traffic in `go-api` except:
- DNS to CoreDNS
- pods in `go-api` to the API on 8080

Anything else calling the API (an ingress controller, Prometheus) or anything the API calls (Postgres) needs its own allow rule. `kubectl port-forward` and kubelet probes are not affected.

## Verify a deploy

```sh
# ArgoCD status: want Synced + Healthy
argocd app get go-k8s-todos            # or: kubectl -n argocd get application go-k8s-todos

# Rollout finished?
kubectl -n go-api rollout status deploy/go-api --timeout=120s

# Running image matches the overlay tag?
kubectl -n go-api get deploy go-api -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'
grep newTag k8s/overlays/prod/kustomization.yaml

# All pods ready, no restarts piling up
kubectl -n go-api get pods -l app=go-api

# Smoke test
kubectl -n go-api port-forward svc/go-api 8080:80 &
curl -fsS localhost:8080/healthz && curl -fsS localhost:8080/readyz && curl -fsS localhost:8080/todos
kill %1
```

If something is off, check `kubectl -n go-api describe pod <pod>` and `kubectl -n go-api logs <pod>`.

## Rollback

Git is the source of truth. ArgoCD `selfHeal` undoes manual `kubectl` changes, so roll back through git.

### Preferred: revert the deploy commit

```sh
git log --oneline -- k8s/overlays/prod/kustomization.yaml   # find the bad "deploy: ..." commit
git revert <sha>
git push
```

ArgoCD syncs the previous tag. Then run the verify steps above.

### Pin a known-good tag

```sh
cd k8s/overlays/prod
kustomize edit set image ghcr.io/abhinandkakkadi/go-k8s-todos=ghcr.io/abhinandkakkadi/go-k8s-todos:sha-<good>
git commit -am "rollback: sha-<good>" && git push
```

### Emergency (git or CI is down)

```sh
argocd app set go-k8s-todos --sync-policy none     # stop selfHeal reverting you
argocd app rollback go-k8s-todos <history-id>      # ids from: argocd app history go-k8s-todos
# or: kubectl -n go-api rollout undo deploy/go-api
```

Afterwards, fix git so it matches the cluster, then turn auto-sync back on:

```sh
argocd app set go-k8s-todos --sync-policy automated --self-heal --auto-prune
```
