# Card manifest contract

`card-article-manifest-v1.json` is the producer-owned compatibility fixture for
the `card-article-manifest/v1` handoff. The deployment stack validates this
exact fixture with both `content-publisher` and `mark2note` before replacing a
running container. Update the fixture whenever the producer contract changes.
