# Security policy

Please report vulnerabilities privately through GitHub's
[private vulnerability reporting](https://github.com/sky-valley/pagelike/security/advisories/new).
Don't open public issues for them. We aim to acknowledge reports within a few
days.

Particularly in scope:
- authorization bypasses: rules, selector-scoped denies, ownership;
- cross-site access: one site reading or writing another, or sessions and keys
  crossing sites;
- escapes from the server-JavaScript sandbox or its resource limits;
- stored or mutation XSS through the storage serializer or Liquid output;
- authoring keys reaching browsers.

Supported versions: the latest release.
