# Why does `brew readall --os=all --arch=all` fail when macOS Intel is omitted?

## The Problem

When decommissioning Intel macOS support in a binary Homebrew formula by defining only Apple Silicon (`on_arm`):

```ruby
on_macos do
  on_arm do
    url "https://..._darwin_arm64.tar.gz"
    sha256 "..."
  end
end
```

Homebrew's CI runner (`brew test-bot --only-tap-syntax`) fails during `brew readall --aliases --os=all --arch=all` with:

```text
Invalid formula (catalina, monterey, etc.): nilbot/tap/agents: formula requires at least a URL
```

## The Cause

`brew readall --os=all --arch=all` simulates formula evaluation across all supported and legacy macOS versions in Homebrew's database (including x86_64 Intel releases such as Catalina, Big Sur, Monterey, Ventura, Sonoma, and Sequoia).

When evaluated in the context of an Intel macOS target, the `on_arm` block does not execute, leaving `url` undefined (`nil`). Homebrew requires that every evaluated matrix target has a valid URL unless top-level restrictions (like `depends_on arch: :arm64`) apply globally.

## The Solution

For multi-platform binary tools that also support Linux x86_64 (`linux/amd64`), `depends_on arch: :arm64` cannot be used globally. The cleanest solution is to cross-compile and include `darwin/amd64` (Intel macOS) alongside `darwin/arm64`, `linux/arm64`, and `linux/amd64`, providing complete matrix URL coverage across all platforms.
