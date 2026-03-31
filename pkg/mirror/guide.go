package mirror

import "fmt"

// ManualSetupGuide returns step-by-step instructions for setting up a mirror
// manually when the provider doesn't support it via API.
func ManualSetupGuide(providerName, direction string) string {
	switch {
	case providerName == "github" && direction == "push":
		return githubPushGuide()
	case providerName == "bitbucket":
		return bitbucketGuide(direction)
	default:
		return genericGuide(providerName, direction)
	}
}

func githubPushGuide() string {
	return `GitHub does not support server-side push mirrors via API.

Options:
  1. Use GitHub Actions to push to the target on every commit:
     - Create .github/workflows/mirror.yml in the source repo
     - Use a step like: git push --mirror <target-url>

  2. Use a local cron job:
     git clone --mirror <source-url> /tmp/mirror-repo.git
     cd /tmp/mirror-repo.git
     git remote set-url --push origin <target-url>
     git fetch -p origin && git push --mirror

  3. If the target supports pull mirrors (e.g., Forgejo/Gitea):
     Configure a pull mirror on the target side instead.
     Use: gitbox mirror add-repo <key> <repo> --direction pull --origin b`
}

func bitbucketGuide(direction string) string {
	return fmt.Sprintf(`Bitbucket does not support %s mirrors via API.

Options:
  1. Use Bitbucket Pipelines to push/pull on a schedule
  2. Use a local cron job with git push --mirror / git fetch
  3. If the other provider supports the opposite direction,
     configure the mirror on that side instead.`, direction)
}

func genericGuide(providerName, direction string) string {
	return fmt.Sprintf(`The %q provider does not support %s mirrors via API.

You can set up a manual mirror using:
  1. A cron job: git clone --mirror <source> && git push --mirror <target>
  2. CI/CD pipelines on either the source or target provider
  3. If the other provider supports %s mirrors via API,
     configure the mirror on that side instead.`, providerName, direction, direction)
}
