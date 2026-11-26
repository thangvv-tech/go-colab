# Security Policy

## Supported versions

Security fixes target the latest tagged release and the default branch.

## Reporting a vulnerability

Do not open a public issue for credential exposure, authentication bypass, runtime takeover, SSH bridge issues, or secrets disclosure.

Report privately to repository maintainers through GitHub's private security advisory flow. Include affected version, reproduction steps, impact, and any mitigations already tested.

Do not attach live OAuth tokens, ADC files, runtime proxy tokens, private SSH keys, or full production logs.

## Credential handling

`go-colab` works with OAuth, Application Default Credentials, runtime proxy tokens, and SSH public keys. Treat all token/session files as secrets. The repository ignores common local credential and state files; users remain responsible for their deployment secret storage and access policies.
