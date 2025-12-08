<template>
  <PageLayout
    :eyebrow="t('components.skill.hero.eyebrow')"
    title="Claude Skill"
  >
    <template #actions>
      <button class="ghost-icon" :title="t('components.skill.actions.refresh')"
        :data-tooltip="t('components.skill.actions.refresh')" :disabled="refreshing" @click="refresh">
        <svg viewBox="0 0 24 24" aria-hidden="true" :class="{ spin: refreshing }">
          <path d="M20.5 8a8.5 8.5 0 10-2.38 7.41" fill="none" stroke="currentColor" stroke-width="1.5"
            stroke-linecap="round" stroke-linejoin="round" />
          <path d="M20.5 4v4h-4" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"
            stroke-linejoin="round" />
        </svg>
      </button>
      <button class="ghost-icon" :title="t('components.skill.repos.open')"
        :data-tooltip="t('components.skill.repos.open')" @click="openRepoModal">
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path d="M5 5h14v6H5zM7 13h10v6H7z" fill="none" stroke="currentColor" stroke-width="1.5"
            stroke-linecap="round" stroke-linejoin="round" />
          <path d="M12 7.5v1M12 15.5v1" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
        </svg>
      </button>
    </template>

    <div style="display: flex; flex-direction: column; gap: var(--spacing-section);">
      <!-- <div class="skill-hero-text">
        <p class="lead">
          {{ t('components.skill.hero.lead') }}
        </p>
      </div> -->

      <section class="skill-list-section">
        <div v-if="loading" class="skill-empty">{{ t('components.skill.list.loading') }}</div>
        <div v-else-if="skills.length === 0" class="skill-empty">{{ t('components.skill.list.empty') }}</div>
        <div v-else class="skill-list">
          <article v-for="skill in skills" :key="skill.key || skill.directory" class="skill-card">
            <div class="skill-card-head">
              <div>
                <p class="skill-card-eyebrow">{{ skill.directory }}</p>
                <!-- <h3>{{ skill.name }}</h3> -->
              </div>
              <div class="skill-card-actions">
                <button type="button" class="ghost-icon sm" :title="t('components.skill.actions.view')"
                  :data-tooltip="t('components.skill.actions.view')" @click="openGithub(skill.readme_url)">
                  <svg viewBox="0 0 24 24" aria-hidden="true">
                    <path d="M12 5h7v7M19 5l-9 9" fill="none" stroke="currentColor" stroke-width="1.6"
                      stroke-linecap="round" stroke-linejoin="round" />
                    <path d="M11 6H7a2 2 0 00-2 2v9a2 2 0 002 2h9a2 2 0 002-2v-4" fill="none" stroke="currentColor"
                      stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
                  </svg>
                </button>
                <button type="button" v-if="skill.installed" class="ghost-icon sm danger"
                  :title="t('components.skill.actions.uninstall')"
                  :data-tooltip="t('components.skill.actions.uninstall')" :disabled="isUninstallingSkill(skill)"
                  @click="handleUninstall(skill)">
                  <svg v-if="!isUninstallingSkill(skill)" viewBox="0 0 24 24" aria-hidden="true">
                    <path d="M5 7h14M10 11v6M14 11v6M9 7V5h6v2" fill="none" stroke="currentColor" stroke-width="1.6"
                      stroke-linecap="round" stroke-linejoin="round" />
                    <path d="M6.5 7l-.5 12a2 2 0 002 2h8a2 2 0 002-2L17.5 7" fill="none" stroke="currentColor"
                      stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
                  </svg>
                  <span v-else class="skill-action-spinner" aria-hidden="true"></span>
                </button>
                <button type="button" v-else class="ghost-icon sm"
                  :title="canInstallSkill(skill) ? t('components.skill.actions.install') : t('components.skill.list.missingRepo')"
                  :data-tooltip="canInstallSkill(skill) ? t('components.skill.actions.install') : t('components.skill.list.missingRepo')"
                  :disabled="isInstallingSkill(skill) || !canInstallSkill(skill)" @click="handleInstall(skill)">
                  <svg v-if="!isInstallingSkill(skill)" viewBox="0 0 24 24" aria-hidden="true">
                    <path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"
                      stroke-linejoin="round" fill="none" />
                  </svg>
                  <span v-else class="skill-action-spinner" aria-hidden="true"></span>
                </button>
              </div>
            </div>
            <p class="skill-card-desc">
              {{ skill.description || t('components.skill.list.noDescription') }}
            </p>
          </article>
        </div>
        <p v-if="skillsError" class="skill-error">{{ skillsError }}</p>
      </section>
    </div>

    <BaseModal :open="repoModalOpen" :title="t('components.skill.repos.title')" @close="closeRepoModal">
      <div class="skill-repo-section repo-modal-content">
        <p class="skill-repo-subtitle">{{ t('components.skill.repos.subtitle') }}</p>
        <form class="skill-repo-form" @submit.prevent="submitRepo">
          <div class="repo-input-field">
            <input v-model="repoForm.url" type="text" :placeholder="t('components.skill.repos.urlPlaceholder')"
              :disabled="repoBusy" />
          </div>
          <div class="repo-form-actions">
            <input v-model="repoForm.branch" type="text" :placeholder="t('components.skill.repos.branchPlaceholder')"
              :disabled="repoBusy" />
            <button class="ghost-icon" type="submit" :disabled="repoBusy" :title="t('components.skill.repos.addLabel')"
              :data-tooltip="t('components.skill.repos.addLabel')">
              <svg viewBox="0 0 24 24" aria-hidden="true" :class="{ spin: repoBusy }">
                <path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"
                  stroke-linejoin="round" fill="none" />
              </svg>
            </button>
          </div>
        </form>
        <p v-if="repoError" class="skill-error">{{ repoError }}</p>
        <div class="skill-repo-list" :class="{ loading: repoLoading }">
          <p v-if="repoLoading" class="skill-empty">{{ t('components.skill.repos.loading') }}</p>
          <p v-else-if="!repoList.length" class="skill-empty">{{ t('components.skill.repos.empty') }}</p>
          <div v-else>
            <article v-for="repo in repoList" :key="repoKey(repo)" class="skill-repo-item">
              <div class="skill-repo-meta">
                <p class="repo-name">{{ repo.owner }}/{{ repo.name }}</p>
                <span class="repo-branch">{{ t('components.skill.repos.branchLabel', { branch: repo.branch }) }}</span>
              </div>
              <div class="skill-repo-actions">
                <button class="ghost-icon sm" type="button" :title="t('components.skill.repos.viewLabel')"
                  :data-tooltip="t('components.skill.repos.viewLabel')" @click="openRepoGithub(repo)">
                  <svg viewBox="0 0 24 24" aria-hidden="true">
                    <path d="M12 5h7v7M19 5l-9 9" fill="none" stroke="currentColor" stroke-width="1.6"
                      stroke-linecap="round" stroke-linejoin="round" />
                    <path d="M11 6H7a2 2 0 00-2 2v9a2 2 0 002 2h9a2 2 0 002-2v-4" fill="none" stroke="currentColor"
                      stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
                  </svg>
                </button>
                <button class="ghost-icon sm danger" type="button" :title="t('components.skill.repos.removeLabel')"
                  :data-tooltip="t('components.skill.repos.removeLabel')" :disabled="repoBusy"
                  @click="removeRepo(repo)">
                  <svg viewBox="0 0 24 24" aria-hidden="true">
                    <path d="M5 7h14M10 11v6M14 11v6M9 7V5h6v2" fill="none" stroke="currentColor" stroke-width="1.6"
                      stroke-linecap="round" stroke-linejoin="round" />
                    <path d="M6.5 7l-.5 12a2 2 0 002 2h8a2 2 0 002-2L17.5 7" fill="none" stroke="currentColor"
                      stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
                  </svg>
                </button>
              </div>
            </article>
          </div>
        </div>
      </div>
    </BaseModal>
  </PageLayout>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import PageLayout from '../common/PageLayout.vue'
import { Browser } from '@wailsio/runtime'
import {
  fetchSkills,
  installSkill,
  uninstallSkill,
  fetchSkillRepos,
  addSkillRepo,
  removeSkillRepo,
  type SkillSummary,
  type SkillRepoConfig
} from '../../services/skill'
import BaseModal from '../common/BaseModal.vue'

const router = useRouter()
const { t } = useI18n()

const skills = ref<SkillSummary[]>([])
const repoList = ref<SkillRepoConfig[]>([])
const repoModalOpen = ref(false)
const loading = ref(false)
const repoLoading = ref(false)
const skillsError = ref('')
const repoError = ref('')
const processingSkill = ref('')
const repoBusy = ref(false)
const repoForm = reactive({ url: '', branch: 'main' })
const skillRepoUrl = 'https://github.com/ComposioHQ/awesome-claude-skills'

const refreshing = computed(() => loading.value || repoLoading.value)

const skillIdentity = (skill: SkillSummary) =>
  skill.key || `${(skill.repo_owner ?? 'local').toLowerCase()}:${skill.directory.toLowerCase()}`

const installProcessingKey = (skill: SkillSummary) => `install:${skillIdentity(skill)}`
const uninstallProcessingKey = (skill: SkillSummary) => `uninstall:${skillIdentity(skill)}`

const isInstallingSkill = (skill: SkillSummary) => processingSkill.value === installProcessingKey(skill)
const isUninstallingSkill = (skill: SkillSummary) => processingSkill.value === uninstallProcessingKey(skill)
const canInstallSkill = (skill: SkillSummary) => Boolean(skill.repo_owner && skill.repo_name)

const updateSkillInstalledFlag = (skill: SkillSummary, installed: boolean) => {
  const key = skillIdentity(skill)
  const target = skills.value.find((item) => skillIdentity(item) === key)
  if (target) {
    target.installed = installed
  }
}

const loadSkills = async () => {
  loading.value = true
  skillsError.value = ''
  try {
    skills.value = await fetchSkills()
  } catch (error) {
    console.error('failed to load skills', error)
    skillsError.value = t('components.skill.list.error')
  } finally {
    loading.value = false
    processingSkill.value = ''
  }
}

const loadRepos = async () => {
  repoLoading.value = true
  repoError.value = ''
  try {
    repoList.value = await fetchSkillRepos()
  } catch (error) {
    console.error('failed to load skill repos', error)
    repoError.value = t('components.skill.repos.loadError')
  } finally {
    repoLoading.value = false
  }
}

const refresh = () => {
  void Promise.all([loadRepos(), loadSkills()])
}

const openRepoModal = () => {
  repoModalOpen.value = true
  if (!repoList.value.length && !repoLoading.value) {
    void loadRepos()
  }
}

const closeRepoModal = () => {
  repoModalOpen.value = false
}

const goHome = () => {
  router.push('/')
}

const openExternal = (target: string) => {
  if (!target) return
  Browser.OpenURL(target).catch(() => {
    console.error('failed to open link', target)
  })
}

const openGithub = (url: string) => {
  if (!url) return
  openExternal(url)
}

const openSkillRepo = () => {
  openExternal(skillRepoUrl)
}

const repoKey = (repo: SkillRepoConfig) => `${repo.owner}/${repo.name}`

const parseRepoInput = (value: string) => {
  let input = value.trim()
  if (!input) return null
  input = input.replace(/^https?:\/\/(www\.)?github\.com\//i, '')
  input = input.replace(/\.git$/i, '')
  const parts = input.split('/')
  if (parts.length < 2) return null
  const owner = parts[0]
  const name = parts[1]
  if (!owner || !name) return null
  return { owner, name }
}

const submitRepo = async () => {
  const parsed = parseRepoInput(repoForm.url)
  if (!parsed) {
    repoError.value = t('components.skill.repos.formError')
    return
  }
  repoBusy.value = true
  repoError.value = ''
  try {
    repoList.value = await addSkillRepo({
      owner: parsed.owner,
      name: parsed.name,
      branch: repoForm.branch || 'main',
      enabled: true
    })
    repoForm.url = ''
    repoForm.branch = 'main'
    await loadSkills()
  } catch (error) {
    console.error('failed to add skill repo', error)
    repoError.value = t('components.skill.repos.addError')
  } finally {
    repoBusy.value = false
  }
}

const removeRepo = async (repo: SkillRepoConfig) => {
  repoBusy.value = true
  repoError.value = ''
  try {
    repoList.value = await removeSkillRepo(repo.owner, repo.name)
    await loadSkills()
  } catch (error) {
    console.error('failed to remove skill repo', error)
    repoError.value = t('components.skill.repos.removeError')
  } finally {
    repoBusy.value = false
  }
}

const openRepoGithub = (repo: SkillRepoConfig) => {
  if (!repo?.owner || !repo?.name) {
    return
  }
  const url = `https://github.com/${repo.owner}/${repo.name}`
  openExternal(url)
}

const handleInstall = async (skill: SkillSummary) => {
  if (!canInstallSkill(skill)) {
    skillsError.value = t('components.skill.list.missingRepo')
    return
  }
  processingSkill.value = installProcessingKey(skill)
  try {
    await installSkill({
      directory: skill.directory,
      repo_owner: skill.repo_owner,
      repo_name: skill.repo_name,
      repo_branch: skill.repo_branch
    })
    updateSkillInstalledFlag(skill, true)
    skillsError.value = ''
  } catch (error) {
    console.error('failed to install skill', error)
    skillsError.value = t('components.skill.actions.installError', { name: skill.name })
  } finally {
    processingSkill.value = ''
  }
}

const handleUninstall = async (skill: SkillSummary) => {
  processingSkill.value = uninstallProcessingKey(skill)
  try {
    await uninstallSkill(skill.directory)
    updateSkillInstalledFlag(skill, false)
    skillsError.value = ''
  } catch (error) {
    console.error('failed to uninstall skill', error)
    skillsError.value = t('components.skill.actions.uninstallError', { name: skill.name })
  } finally {
    processingSkill.value = ''
  }
}

onMounted(() => {
  void Promise.all([loadRepos(), loadSkills()])
})
</script>

<style scoped>
.skill-page {
  gap: 32px;
  color: var(--mac-text);
}

.skill-repo-section {
  border: 1px solid var(--mac-border);
  border-radius: 12px;
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 16px;
  background: color-mix(in srgb, var(--mac-surface) 90%, transparent);
}

.skill-repo-header {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  justify-content: space-between;
  align-items: center;
}

.skill-repo-header h2 {
  margin: 0;
  font-size: 1.05rem;
}

.skill-repo-header p {
  margin: 4px 0 0;
  color: var(--mac-text-secondary);
}

.skill-repo-subtitle {
  margin: 0 0 12px;
  color: var(--mac-text-secondary);
  font-size: 0.95rem;
}

.skill-repo-form {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  width: 100%;
}

.repo-input-field {
  flex: 1;
  min-width: 220px;
}

.skill-repo-form input {
  border: 1px solid var(--mac-border);
  border-radius: 12px;
  padding: 8px 12px;
  background: var(--mac-surface);
  color: var(--mac-text);
  font-size: 0.9rem;
}

.repo-input-field input {
  width: 100%;
}

.repo-form-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  align-items: center;
}

.repo-form-actions input {
  width: 160px;
}

.skill-repo-actions {
  display: flex;
  gap: 8px;
  align-items: center;
}

.skill-repo-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.skill-repo-list.loading {
  opacity: 0.7;
}

.skill-repo-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 18px;
  border: 1px solid var(--mac-border);
  border-radius: 12px;
  background: color-mix(in srgb, var(--mac-surface) 80%, transparent);
  gap: 16px;
  margin: 0 0 8px;
}

.skill-repo-item:last-child {
  margin-bottom: 0;
}

.skill-repo-meta {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.skill-repo-meta .repo-name {
  margin: 0;
  font-weight: 600;
}

.skill-repo-meta .repo-branch {
  font-size: 0.85rem;
  color: var(--mac-text-secondary);
}

.repo-modal-content {
  min-width: min(600px, 80vw);
}


.skill-hero {
  margin: 12px 0 12px;
}

.skill-lead {
  color: var(--mac-text-secondary);
  font-size: 0.95rem;
  line-height: 1.5;
}

.skill-link {
  color: var(--mac-accent);
  font-weight: 600;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 0;
  margin-left: 4px;
}

.skill-link:focus-visible {
  outline: none;
  text-decoration: underline;
}

.skill-link svg {
  width: 16px;
  height: 16px;
}

.skill-link:hover {
  text-decoration: underline;
}

.skill-hero h1 {
  font-size: clamp(26px, 3vw, 34px);
  margin-bottom: 8px;
}

.skill-button {
  border: none;
  border-radius: 999px;
  padding: 8px 20px;
  font-weight: 600;
  font-size: 0.95rem;
  cursor: pointer;
  background: #2563eb;
  color: white;
  transition: opacity 0.2s ease;
}

.ghost-icon svg.spin {
  animation: skill-spin 1s linear infinite;
}

.skill-button:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.skill-button.ghost {
  background: transparent;
  border: 1px solid rgba(148, 163, 184, 0.4);
  color: #e2e8f0;
}

.skill-button.danger {
  background: #dc2626;
}

.skill-list-section {
  margin-top: 0px;
}


.skill-empty {
  margin-top: 32px;
  color: var(--mac-text-secondary);
  text-align: center;
}

.skill-repo-list .skill-empty {
  margin-top: 0;
}

.skill-list {
  margin-top: 8px;
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(400px, 1fr));
  gap: 24px;
}

.skill-card {
  background: color-mix(in srgb, var(--mac-surface) 90%, transparent);
  border: 1px solid var(--mac-border);
  border-radius: 12px;
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.skill-card-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}

.skill-card-eyebrow {
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.18em;
  color: var(--mac-text-secondary);
  margin-bottom: 4px;
}

.skill-card h3 {
  font-size: 1rem;
  margin: 0 0 4px;
}

.skill-card-desc {
  color: var(--mac-text-secondary);
  min-height: 50px;
  font-size: 0.9rem;
  line-height: 1.5;
  margin-top: 8px;
}


.skill-card-actions {
  display: flex;
  gap: 6px;
  flex-wrap: nowrap;
}

.skill-card-actions .ghost-icon {
  width: 32px;
  height: 32px;
}

.skill-card-actions .ghost-icon svg {
  width: 18px;
  height: 18px;
}

.skill-card-actions .ghost-icon.danger {
  color: #ef4444;
}

.skill-card-actions .ghost-icon:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.ghost-icon:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.skill-action-spinner {
  width: 16px;
  height: 16px;
  border-radius: 50%;
  border: 2px solid currentColor;
  border-top-color: transparent;
  animation: skill-spin 0.8s linear infinite;
  display: inline-block;
}

.skill-error {
  color: #f87171;
  margin-top: 16px;
}

.skill-page :where(button, h1, h2, h3, p) {
  transition: color 0.2s ease, background 0.2s ease, border-color 0.2s ease;
}

html.dark .skill-card {
  background: color-mix(in srgb, var(--mac-surface) 70%, transparent);
}

html.dark .skill-button.ghost {
  border-color: rgba(255, 255, 255, 0.2);
  color: var(--mac-text);
}

html.dark .skill-card-desc {
  color: rgba(248, 250, 252, 0.8);
}

html.dark .skill-card-eyebrow {
  color: rgba(248, 250, 252, 0.6);
}

@media (max-width: 768px) {
  .skill-hero {
    flex-direction: column;
  }

  .skill-card {
    padding: 20px;
  }

  .skill-button {
    flex: 1;
    text-align: center;
  }

}

@keyframes skill-spin {
  from {
    transform: rotate(0deg);
  }

  to {
    transform: rotate(360deg);
  }
}
</style>
