Closes #<issue-number>

### Summary

- What and why in 1–3 sentences.

### Checklist

- [ ] Ran `make test_e2e` locally and passed
- [ ] If release workflow changed, ran `make dev_release DEVICE_IP=<ip>` and/or `make test_production_release DEVICE_IP=<ip> SIGNING_KEY_FPR=<fingerprint>`
- [ ] Linked to issue(s) above by issue number (e.g. `Closes #<issue-number>`)
- [ ] One problem per PR (no unrelated changes)
- [ ] Lints pass; CI green
- [ ] Tricky parts are commented in code
