// Named aliases for the generated OpenAPI schemas (src/api/schema.ts, `npm run gen:api`).
// A renamed or removed schema in backend/api/openapi.yaml breaks the typecheck here.
import type { components } from './schema.ts'

type Schemas = components['schemas']

export type ApiErrorBody = Schemas['Error']
export type Role = Schemas['Role']
export type User = Schemas['User']
export type Session = Schemas['Session']

export type Building = Schemas['Building']
export type BuildingInput = Schemas['BuildingInput']
export type RoomType = Schemas['RoomType']
export type Room = Schemas['Room']
export type RoomInput = Schemas['RoomInput']
export type Group = Schemas['Group']
export type GroupInput = Schemas['GroupInput']
export type Subgroup = Schemas['Subgroup']
export type SubgroupInput = Schemas['SubgroupInput']
export type Teacher = Schemas['Teacher']
export type TeacherInput = Schemas['TeacherInput']
export type Discipline = Schemas['Discipline']
export type DisciplineInput = Schemas['DisciplineInput']

export type Parity = Schemas['Parity']
export type AvailabilityStatus = Schemas['AvailabilityEntry']['status']
export type AvailabilityEntry = Schemas['AvailabilityEntry']
export type Availability = Schemas['Availability']
export type TimeGrid = Schemas['TimeGrid']
export type Period = Schemas['Period']

export type AudienceMember = Schemas['AudienceMember']
export type Lesson = Schemas['Lesson']
export type LessonKind = Schemas['LessonKind']
export type CurriculumItem = Schemas['CurriculumItem']
export type CurriculumItemInput = Schemas['CurriculumItemInput']
