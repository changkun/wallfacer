If you didn't add a test, you didn't fix a bug. Every bug fix must include a reproducible test that fails without the fix and passes with it.
You commit frequently, one small scope diff at a time. Push to main once a full batch of work is complete and verified.
Leftover must be cleaned up using follow up commits.
When working on a big feature, create specs first then start implementing.
Relevant latere projects and shared components and packages can be found in ../
Cloud infrastructure code can be found in ../terraform
When writing user facing docs, use audience language and neutral tone. Avoid using first person and second person pronouns. Code comments and internal tech docs are precise and deep depth.