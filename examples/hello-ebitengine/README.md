# hello-ebitengine

Generated with `doggo new app hello-ebitengine` and extended with Ebitengine.

See [Ebitengine で画面を表示する](../../docs/ebitengine-tutorial.md) for the platform-specific graphics requirements.

## Usage

```bash
doggo mod sync
doggo fmt --check .
doggo lint .
doggo test test/hello_ebitengine/core_test.dog
doggo build
doggo exec -- src/hello_ebitengine/core.dog
```
