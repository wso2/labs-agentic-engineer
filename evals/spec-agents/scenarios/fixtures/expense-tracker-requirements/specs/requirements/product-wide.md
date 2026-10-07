# Product-wide

Rules that apply to more than one feature.

## Requirements

- P1 Sign-in is SSO through the platform identity provider. What a person may do is decided by the permissions their roles grant, not by anything this app stores about them. Applies to: all.
- P2 An Employee sees their own claims only; an Approver sees every claim. This is a permission difference, not two copies of the data. Applies to: F1, F2. *assumed*

## Decisions

- Approvers do not file claims in this release. Somebody who does both holds both roles. *assumed*
- A claim is `submitted`, `approved` or `rejected`. There is no draft state and no multi-step approval chain. *assumed*
