export const meta = {
  name: 'sprint-kickoff',
  description: 'Create GitHub issues, labels, milestones, and project board for a given sprint from the roadmap docs',
  phases: [
    { title: 'Parse', detail: 'Read ROADMAP_AND_SPRINTS.md and extract sprint stories' },
    { title: 'Prepare', detail: 'Create labels, milestones, project board if missing' },
    { title: 'Create', detail: 'Create GitHub issues for each story with metadata' },
    { title: 'Organize', detail: 'Assign issues to sprint milestone and project board columns' },
    { title: 'Report', detail: 'Summarize created issues with links' },
  ],
}

// Usage: args = "0.1" for sprint 0.1, "1.3" for sprint 1.3, or "all" for all sprints
const target = args || 'all'
log(`Sprint kickoff target: ${target}`)

// Phase 1: Parse roadmap doc (haiku — simple extraction)
phase('Parse')
const roadmapText = await agent(`Read /mnt/data/git/openagentplatform/docs/architecture/ROADMAP_AND_SPRINTS.md and extract stories for sprint(s): ${target === 'all' ? 'ALL sprints' : target}.

For each story, return:
- sprint (e.g., 0.1, 0.2, 1.1)
- story_number (e.g., 0.1.1, 1.1.2)
- title
- role (as_a)
- feature (i_want)
- benefit (so_that)
- acceptance_criteria (array of strings)
- complexity (S/M/L/XL)
- stream (A/B/C/D)
- dependencies (array of story_numbers)
- related_docs (array of file paths from docs/architecture/ relevant to this story)

If target is "all", return every sprint story from the document.
If target is a specific sprint, return only that sprint's stories.`, {
  label: 'parse-roadmap',
  phase: 'Parse',
  model: 'haiku',
  schema: {
    type: 'object',
    properties: {
      stories: {
        type: 'array',
        items: {
          type: 'object',
          properties: {
            sprint: { type: 'string' },
            story_number: { type: 'string' },
            title: { type: 'string' },
            role: { type: 'string' },
            feature: { type: 'string' },
            benefit: { type: 'string' },
            acceptance_criteria: { type: 'array', items: { type: 'string' } },
            complexity: { type: 'string', enum: ['S', 'M', 'L', 'XL'] },
            stream: { type: 'string', enum: ['A', 'B', 'C', 'D'] },
            dependencies: { type: 'array', items: { type: 'string' } },
            related_docs: { type: 'array', items: { type: 'string' } },
          },
          required: ['sprint', 'story_number', 'title', 'role', 'feature', 'benefit', 'acceptance_criteria', 'complexity', 'stream'],
        },
      },
    },
    required: ['stories'],
  },
})

if (!roadmapText || !roadmapText.stories || roadmapText.stories.length === 0) {
  throw new Error(`No stories found for sprint ${target}. Check ROADMAP_AND_SPRINTS.md format.`)
}

log(`Parsed ${roadmapText.stories.length} stories for sprint(s) ${target}`)

// Phase 2: Prepare labels, milestones, project (sonnet — reasoning over GitHub ops)
phase('Prepare')

const milestonesToCreate = [...new Set(roadmapText.stories.map(s => s.sprint))]

const prepResults = await parallel([
  // Create labels
  () => agent(`Ensure GitHub labels exist for TheArchitectit/openagentplatform repository. Create labels via gh CLI if missing.
Create these labels with colors:
- stream/backend : 1d76db
- stream/agent : 67b587
- stream/frontend : a371f7
- stream/infra : f68406
- complexity/S : 0e8a16
- complexity/M : dfb091
- complexity/L : d93f0b
- complexity/XL : 6e5494
- phase/0 : c2e0c6
- phase/1 : c5def5
- phase/2 : ffdabf
- phase/3 : ffd8b1
- phase/4 : fbca04
- phase/5 : f97583
- phase/6 : d4c5f9

Use gh api repos/TheArchitectit/openagentplatform/labels to list existing labels.
Use gh label create only for labels that don't exist.
Return structured list of created vs existing labels.`, {
    label: 'prepare:labels',
    phase: 'Prepare',
    model: 'sonnet',
  }),
  // Create milestones per sprint
  () => agent(`Create GitHub milestones for sprints: ${milestonesToCreate.join(', ')} in TheArchitectit/openagentplatform.
Use gh api repos/TheArchitectit/openagentplatform/milestones to list existing milestones first, then create any missing ones.
Title format: "Sprint X.Y". Description: "OpenAgentPlatform Sprint X.Y".
Due dates:
- Sprint 0.1: 2026-06-29
- Sprint 0.2: 2026-07-13
- Sprint 1.1: 2026-07-27
- Sprint 1.2: 2026-08-10
- Sprint 1.3: 2026-08-24
- Sprint 1.4: 2026-09-07
- Sprint 1.5: 2026-09-21

Return created milestone numbers as {numbers: [1, 2, ...]}.`, {
    label: 'prepare:milestones',
    phase: 'Prepare',
    model: 'sonnet',
  }),
  // Create project board if needed
  () => agent(`Ensure a GitHub project named "OpenAgentPlatform Development" exists for repository TheArchitectit/openagentplatform.
Use gh project list --owner TheArchitectit to check, then gh project create if missing.
Report the project number as {project_number: N}.`, {
    label: 'prepare:project',
    phase: 'Prepare',
    model: 'sonnet',
  }),
])

const createdMilestoneNumbers = (await prepResults[1])?.numbers || []
log(`Preparation complete. Milestones for sprint ${target}: ${createdMilestoneNumbers.join(', ')}`)

// Phase 3: Create GitHub issues (sonnet — structured issue creation)
phase('Create')

function streamName(stream) {
  const names = { A: 'backend', B: 'agent', C: 'frontend', D: 'infra' }
  return names[stream] || 'general'
}

const createdIssues = await parallel(roadmapText.stories.map(story => () =>
  agent(`Create a GitHub issue in TheArchitectit/openagentplatform for this story. Use the gh CLI.

Required behavior:
1. Create issue with title "[Sprint ${story.sprint}] ${story.title.replace(/"/g, '\\"')}"
2. Body must include markdown:
   ## Story
   **As a** ${story.role}
   **I want** ${story.feature}
   **So that** ${story.benefit}

   ## Acceptance Criteria
${story.acceptance_criteria.map(c => `   - [ ] ${c}`).join('\n')}

   ## Metadata
   - **Sprint:** ${story.sprint}
   - **Story Number:** ${story.story_number}
   - **Complexity:** ${story.complexity}
   - **Stream:** ${story.stream}
   - **Dependencies:** ${(story.dependencies || []).join(', ') || 'None'}

   ## Related Architecture Docs
${(story.related_docs || []).map(d => `   - ${d}`).join('\n') || '   - docs/architecture/ROADMAP_AND_SPRINTS.md'}
3. Apply labels: phase/${story.sprint.split('.')[0]}, stream/${streamName(story.stream)}, complexity/${story.complexity}
4. Apply milestone: "Sprint ${story.sprint}"
5. Return {issue_number: N, issue_url: "...", title: "...", sprint: "${story.sprint}", story_number: "${story.story_number}"}

Use gh issue create with --label and --milestone. Use proper shell escaping.`, {
    label: `create-issue:${story.story_number}`,
    phase: 'Create',
    model: 'sonnet',
    schema: {
      type: 'object',
      properties: {
        issue_number: { type: 'integer' },
        issue_url: { type: 'string' },
        title: { type: 'string' },
        sprint: { type: 'string' },
        story_number: { type: 'string' },
      },
      required: ['issue_number', 'issue_url', 'title', 'sprint', 'story_number'],
    },
  })
))

log(`Created ${createdIssues.filter(Boolean).length} issues`)

// Phase 4: Organize issues into project board (haiku — simple bulk add)
phase('Organize')

const projectNumber = (await prepResults[2])?.project_number || 1
const issueNodes = createdIssues.filter(Boolean)

if (issueNodes.length > 0 && projectNumber) {
  await agent(`Add these GitHub issues to project #${projectNumber} for TheArchitectit/openagentplatform, in the "Todo" status:
${issueNodes.map(i => `- #${i.issue_number}: ${i.issue_url}`).join('\n')}

Use gh project item-add ${projectNumber} --owner TheArchitectit --url <issue_url> for each issue. If "Todo" is not the default status, also use gh project item-edit to set Status field to "Todo".
Report how many issues were added.`, {
    label: 'organize:project-board',
    phase: 'Organize',
    model: 'haiku',
  })
}

// Phase 5: Report (sonnet — synthesis)
phase('Report')

const summary = await agent(`Write a human-readable summary of the sprint kickoff. The data:

Target sprint(s): ${target}
Stories parsed: ${roadmapText.stories.length}
Issues created: ${issueNodes.length}

By stream:
${JSON.stringify(issueNodes.reduce((acc, i) => {
  const story = roadmapText.stories.find(s => s.story_number === i.story_number)
  const stream = story?.stream || '?'
  acc[stream] = (acc[stream] || 0) + 1
  return acc
}, {}), null, 2)}

By complexity:
${JSON.stringify(issueNodes.reduce((acc, i) => {
  const story = roadmapText.stories.find(s => s.story_number === i.story_number)
  const complexity = story?.complexity || '?'
  acc[complexity] = (acc[complexity] || 0) + 1
  return acc
}, {}), null, 2)}

Issue links:
${issueNodes.map(i => `- #${i.issue_number}: ${i.title} (${i.issue_url})`).join('\n')}

Return a clean markdown summary with counts, breakdowns, and the next steps for the team.`, {
  label: 'report:summary',
  phase: 'Report',
  model: 'sonnet',
})

return summary