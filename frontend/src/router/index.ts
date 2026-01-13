import { createRouter, createWebHashHistory } from 'vue-router'
import MainPage from '../components/Main/Index.vue'
import DashboardPage from '../components/Dashboard/Index.vue'
import LogsPage from '../components/Logs/Index.vue'
import GeneralPage from '../components/General/Index.vue'
import McpPage from '../components/Mcp/index.vue'
import SkillPage from '../components/Skill/Index.vue'
import PromptsPage from '../components/Prompts/Index.vue'
import SpeedTestPage from '../components/SpeedTest/Index.vue'
import EnvCheckPage from '../components/EnvCheck/Index.vue'
import ConsolePage from '../components/Console/Index.vue'
import AvailabilityPage from '../components/Availability/Index.vue'
import MitmPocPage from '../components/MitmPoc/Index.vue'

const routes = [
  { path: '/', component: MainPage },
  { path: '/dashboard', component: DashboardPage },
  { path: '/prompts', component: PromptsPage },
  { path: '/mcp', component: McpPage },
  { path: '/skill', component: SkillPage },
  { path: '/availability', component: AvailabilityPage },
  { path: '/speedtest', component: SpeedTestPage },
  { path: '/env', component: EnvCheckPage },
  { path: '/console', component: ConsolePage },
  { path: '/logs', component: LogsPage },
  { path: '/mitm-poc', component: MitmPocPage },
  { path: '/settings', component: GeneralPage },
]

export default createRouter({
  history: createWebHashHistory(), // Use createWebHashHistory for hash-based routing
  routes
})
