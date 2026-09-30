package api

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/istu-pro-dev/timetable/backend/internal/store"
	"github.com/istu-pro-dev/timetable/backend/internal/store/db"
)

// Limits of reference data values.
const (
	maxNameLen     = 200
	maxAddressLen  = 500
	maxDivisionLen = 50
	maxCapacity    = 100000
	maxGroupSize   = 10000
	maxPart        = 100
)

// ---------------------------------------------------------------- buildings

type building struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
}

type buildingInput struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

func (in *buildingInput) validate() error {
	var v validator
	v.text("name", &in.Name, 1, maxNameLen)
	v.text("address", &in.Address, 0, maxAddressLen)
	return v.err()
}

func toBuilding(b db.Building) building { return building{ID: b.ID, Name: b.Name, Address: b.Address} }

func (s *server) listBuildings(w http.ResponseWriter, r *http.Request) {
	s.read(w, r, func(ctx context.Context, q *db.Queries) (any, error) {
		rows, err := q.ListBuildings(ctx)
		return mapSlice(rows, toBuilding), err
	})
}

func (s *server) getBuilding(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.writeError(w, err, opRead)
		return
	}
	s.read(w, r, func(ctx context.Context, q *db.Queries) (any, error) {
		b, err := q.GetBuilding(ctx, id)
		return toBuilding(b), orNotFound(err, "building")
	})
}

func (s *server) createBuilding(w http.ResponseWriter, r *http.Request) {
	var in buildingInput
	if !s.decode(w, r, &in) {
		return
	}
	s.mutate(w, r, mutation{entity: "building", op: opWrite, status: http.StatusCreated,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			b, err := q.CreateBuilding(ctx, db.CreateBuildingParams{Name: in.Name, Address: in.Address})
			if err != nil {
				return nil, err
			}
			out := toBuilding(b)
			c.EntityID, c.After = idString(b.ID), out
			return out, nil
		}})
}

func (s *server) updateBuilding(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.writeError(w, err, opWrite)
		return
	}
	var in buildingInput
	if !s.decode(w, r, &in) {
		return
	}
	s.mutate(w, r, mutation{entity: "building", entityID: idString(id), op: opWrite, status: http.StatusOK,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			before, err := q.GetBuilding(ctx, id)
			if err != nil {
				return nil, orNotFound(err, "building")
			}
			b, err := q.UpdateBuilding(ctx, db.UpdateBuildingParams{ID: id, Name: in.Name, Address: in.Address})
			if err != nil {
				return nil, err
			}
			out := toBuilding(b)
			c.Before, c.After = toBuilding(before), out
			return out, nil
		}})
}

func (s *server) deleteBuilding(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.writeError(w, err, opDelete)
		return
	}
	s.mutate(w, r, mutation{entity: "building", entityID: idString(id), op: opDelete, status: http.StatusNoContent,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			before, err := q.GetBuilding(ctx, id)
			if err != nil {
				return nil, orNotFound(err, "building")
			}
			c.Before = toBuilding(before)
			_, err = q.DeleteBuilding(ctx, id)
			return nil, err
		}})
}

// ---------------------------------------------------------------- room types

type roomType struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type roomTypeCreate struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

func (in *roomTypeCreate) validate() error {
	var v validator
	v.code("code", in.Code)
	v.text("name", &in.Name, 1, maxNameLen)
	return v.err()
}

type roomTypeUpdate struct {
	Name string `json:"name"`
}

func (in *roomTypeUpdate) validate() error {
	var v validator
	v.text("name", &in.Name, 1, maxNameLen)
	return v.err()
}

func toRoomType(t db.RoomType) roomType { return roomType{Code: t.Code, Name: t.Name} }

func (s *server) listRoomTypes(w http.ResponseWriter, r *http.Request) {
	s.read(w, r, func(ctx context.Context, q *db.Queries) (any, error) {
		rows, err := q.ListRoomTypes(ctx)
		return mapSlice(rows, toRoomType), err
	})
}

func (s *server) getRoomType(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	s.read(w, r, func(ctx context.Context, q *db.Queries) (any, error) {
		t, err := q.GetRoomType(ctx, code)
		return toRoomType(t), orNotFound(err, "room type")
	})
}

func (s *server) createRoomType(w http.ResponseWriter, r *http.Request) {
	var in roomTypeCreate
	if !s.decode(w, r, &in) {
		return
	}
	s.mutate(w, r, mutation{entity: "room_type", entityID: in.Code, op: opWrite, status: http.StatusCreated,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			t, err := q.CreateRoomType(ctx, db.CreateRoomTypeParams{Code: in.Code, Name: in.Name})
			if err != nil {
				return nil, err
			}
			out := toRoomType(t)
			c.After = out
			return out, nil
		}})
}

func (s *server) updateRoomType(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	var in roomTypeUpdate
	if !s.decode(w, r, &in) {
		return
	}
	s.mutate(w, r, mutation{entity: "room_type", entityID: code, op: opWrite, status: http.StatusOK,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			before, err := q.GetRoomType(ctx, code)
			if err != nil {
				return nil, orNotFound(err, "room type")
			}
			t, err := q.UpdateRoomType(ctx, db.UpdateRoomTypeParams{Code: code, Name: in.Name})
			if err != nil {
				return nil, err
			}
			out := toRoomType(t)
			c.Before, c.After = toRoomType(before), out
			return out, nil
		}})
}

func (s *server) deleteRoomType(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	s.mutate(w, r, mutation{entity: "room_type", entityID: code, op: opDelete, status: http.StatusNoContent,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			before, err := q.GetRoomType(ctx, code)
			if err != nil {
				return nil, orNotFound(err, "room type")
			}
			c.Before = toRoomType(before)
			_, err = q.DeleteRoomType(ctx, code)
			return nil, err
		}})
}

// ---------------------------------------------------------------- rooms

type room struct {
	ID         int64  `json:"id"`
	BuildingID int64  `json:"building_id"`
	Name       string `json:"name"`
	RoomType   string `json:"room_type"`
	Capacity   int32  `json:"capacity"`
}

type roomInput struct {
	BuildingID int64  `json:"building_id"`
	Name       string `json:"name"`
	RoomType   string `json:"room_type"`
	Capacity   int32  `json:"capacity"`
}

func (in *roomInput) validate() error {
	var v validator
	v.positive("building_id", in.BuildingID)
	v.text("name", &in.Name, 1, maxNameLen)
	v.code("room_type", in.RoomType)
	v.between("capacity", int64(in.Capacity), 1, maxCapacity)
	return v.err()
}

func toRoom(r db.Room) room {
	return room{ID: r.ID, BuildingID: r.BuildingID, Name: r.Name, RoomType: r.RoomType, Capacity: r.Capacity}
}

func (s *server) listRooms(w http.ResponseWriter, r *http.Request) {
	buildingID, err := queryID(r, "building_id")
	if err != nil {
		s.writeError(w, err, opRead)
		return
	}
	var roomType pgtype.Text
	if rt := r.URL.Query().Get("room_type"); rt != "" {
		roomType = pgtype.Text{String: rt, Valid: true}
	}
	s.read(w, r, func(ctx context.Context, q *db.Queries) (any, error) {
		rows, err := q.ListRoomsFiltered(ctx, db.ListRoomsFilteredParams{BuildingID: buildingID, RoomType: roomType})
		return mapSlice(rows, toRoom), err
	})
}

func (s *server) getRoom(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.writeError(w, err, opRead)
		return
	}
	s.read(w, r, func(ctx context.Context, q *db.Queries) (any, error) {
		rm, err := q.GetRoom(ctx, id)
		return toRoom(rm), orNotFound(err, "room")
	})
}

func (s *server) createRoom(w http.ResponseWriter, r *http.Request) {
	var in roomInput
	if !s.decode(w, r, &in) {
		return
	}
	s.mutate(w, r, mutation{entity: "room", op: opWrite, status: http.StatusCreated,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			rm, err := q.CreateRoom(ctx, db.CreateRoomParams{
				BuildingID: in.BuildingID, Name: in.Name, RoomType: in.RoomType, Capacity: in.Capacity,
			})
			if err != nil {
				return nil, err
			}
			out := toRoom(rm)
			c.EntityID, c.After = idString(rm.ID), out
			return out, nil
		}})
}

func (s *server) updateRoom(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.writeError(w, err, opWrite)
		return
	}
	var in roomInput
	if !s.decode(w, r, &in) {
		return
	}
	s.mutate(w, r, mutation{entity: "room", entityID: idString(id), op: opWrite, status: http.StatusOK,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			before, err := q.GetRoom(ctx, id)
			if err != nil {
				return nil, orNotFound(err, "room")
			}
			rm, err := q.UpdateRoom(ctx, db.UpdateRoomParams{
				ID: id, BuildingID: in.BuildingID, Name: in.Name, RoomType: in.RoomType, Capacity: in.Capacity,
			})
			if err != nil {
				return nil, err
			}
			out := toRoom(rm)
			c.Before, c.After = toRoom(before), out
			return out, nil
		}})
}

func (s *server) deleteRoom(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.writeError(w, err, opDelete)
		return
	}
	s.mutate(w, r, mutation{entity: "room", entityID: idString(id), op: opDelete, status: http.StatusNoContent,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			before, err := q.GetRoom(ctx, id)
			if err != nil {
				return nil, orNotFound(err, "room")
			}
			c.Before = toRoom(before)
			_, err = q.DeleteRoom(ctx, id)
			return nil, err
		}})
}

// ---------------------------------------------------------------- groups and subgroups

type group struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Course int16  `json:"course"`
	Size   int32  `json:"size"`
}

type groupInput struct {
	Name   string `json:"name"`
	Course *int16 `json:"course"`
	Size   int32  `json:"size"`
}

func (in *groupInput) validate() error {
	var v validator
	v.text("name", &in.Name, 1, maxNameLen)
	if in.Course == nil {
		one := int16(1)
		in.Course = &one
	}
	v.between("course", int64(*in.Course), 1, 6)
	v.between("size", int64(in.Size), 0, maxGroupSize)
	return v.err()
}

func toGroup(g db.Group) group { return group{ID: g.ID, Name: g.Name, Course: g.Course, Size: g.Size} }

func (s *server) listGroups(w http.ResponseWriter, r *http.Request) {
	s.read(w, r, func(ctx context.Context, q *db.Queries) (any, error) {
		rows, err := q.ListGroups(ctx)
		return mapSlice(rows, toGroup), err
	})
}

func (s *server) getGroup(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.writeError(w, err, opRead)
		return
	}
	s.read(w, r, func(ctx context.Context, q *db.Queries) (any, error) {
		g, err := q.GetGroup(ctx, id)
		return toGroup(g), orNotFound(err, "group")
	})
}

func (s *server) createGroup(w http.ResponseWriter, r *http.Request) {
	var in groupInput
	if !s.decode(w, r, &in) {
		return
	}
	s.mutate(w, r, mutation{entity: "group", op: opWrite, status: http.StatusCreated,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			g, err := q.CreateGroup(ctx, db.CreateGroupParams{Name: in.Name, Course: *in.Course, Size: in.Size})
			if err != nil {
				return nil, err
			}
			out := toGroup(g)
			c.EntityID, c.After = idString(g.ID), out
			return out, nil
		}})
}

func (s *server) updateGroup(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.writeError(w, err, opWrite)
		return
	}
	var in groupInput
	if !s.decode(w, r, &in) {
		return
	}
	s.mutate(w, r, mutation{entity: "group", entityID: idString(id), op: opWrite, status: http.StatusOK,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			before, err := q.GetGroup(ctx, id)
			if err != nil {
				return nil, orNotFound(err, "group")
			}
			g, err := q.UpdateGroup(ctx, db.UpdateGroupParams{ID: id, Name: in.Name, Course: *in.Course, Size: in.Size})
			if err != nil {
				return nil, err
			}
			out := toGroup(g)
			c.Before, c.After = toGroup(before), out
			return out, nil
		}})
}

func (s *server) deleteGroup(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.writeError(w, err, opDelete)
		return
	}
	s.mutate(w, r, mutation{entity: "group", entityID: idString(id), op: opDelete, status: http.StatusNoContent,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			before, err := q.GetGroup(ctx, id)
			if err != nil {
				return nil, orNotFound(err, "group")
			}
			c.Before = toGroup(before)
			_, err = q.DeleteGroup(ctx, id)
			return nil, err
		}})
}

type subgroup struct {
	ID       int64  `json:"id"`
	GroupID  int64  `json:"group_id"`
	Division string `json:"division"`
	Part     int16  `json:"part"`
	Size     int32  `json:"size"`
}

type subgroupInput struct {
	Division string `json:"division"`
	Part     int16  `json:"part"`
	Size     int32  `json:"size"`
}

func (in *subgroupInput) validate() error {
	var v validator
	v.text("division", &in.Division, 1, maxDivisionLen)
	v.between("part", int64(in.Part), 1, maxPart)
	v.between("size", int64(in.Size), 0, maxGroupSize)
	return v.err()
}

func toSubgroup(g db.Subgroup) subgroup {
	return subgroup{ID: g.ID, GroupID: g.GroupID, Division: g.Division, Part: g.Part, Size: g.Size}
}

// subgroupPath parses {id} (group) and, when withSub, {subgroup_id}.
func subgroupPath(r *http.Request, withSub bool) (groupID, subID int64, err error) {
	if groupID, err = pathID(r, "id"); err != nil || !withSub {
		return groupID, 0, err
	}
	subID, err = pathID(r, "subgroup_id")
	return groupID, subID, err
}

func (s *server) listSubgroups(w http.ResponseWriter, r *http.Request) {
	groupID, _, err := subgroupPath(r, false)
	if err != nil {
		s.writeError(w, err, opRead)
		return
	}
	s.read(w, r, func(ctx context.Context, q *db.Queries) (any, error) {
		if _, err := q.GetGroup(ctx, groupID); err != nil {
			return nil, orNotFound(err, "group")
		}
		rows, err := q.ListSubgroupsOf(ctx, groupID)
		return mapSlice(rows, toSubgroup), err
	})
}

func (s *server) getSubgroup(w http.ResponseWriter, r *http.Request) {
	groupID, subID, err := subgroupPath(r, true)
	if err != nil {
		s.writeError(w, err, opRead)
		return
	}
	s.read(w, r, func(ctx context.Context, q *db.Queries) (any, error) {
		g, err := q.GetSubgroupOf(ctx, db.GetSubgroupOfParams{GroupID: groupID, ID: subID})
		return toSubgroup(g), orNotFound(err, "subgroup")
	})
}

func (s *server) createSubgroup(w http.ResponseWriter, r *http.Request) {
	groupID, _, err := subgroupPath(r, false)
	if err != nil {
		s.writeError(w, err, opWrite)
		return
	}
	var in subgroupInput
	if !s.decode(w, r, &in) {
		return
	}
	s.mutate(w, r, mutation{entity: "subgroup", op: opWrite, status: http.StatusCreated,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			if _, err := q.GetGroup(ctx, groupID); err != nil {
				return nil, orNotFound(err, "group")
			}
			g, err := q.CreateSubgroup(ctx, db.CreateSubgroupParams{
				GroupID: groupID, Division: in.Division, Part: in.Part, Size: in.Size,
			})
			if err != nil {
				return nil, err
			}
			out := toSubgroup(g)
			c.EntityID, c.After = idString(g.ID), out
			return out, nil
		}})
}

func (s *server) updateSubgroup(w http.ResponseWriter, r *http.Request) {
	groupID, subID, err := subgroupPath(r, true)
	if err != nil {
		s.writeError(w, err, opWrite)
		return
	}
	var in subgroupInput
	if !s.decode(w, r, &in) {
		return
	}
	s.mutate(w, r, mutation{entity: "subgroup", entityID: idString(subID), op: opWrite, status: http.StatusOK,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			before, err := q.GetSubgroupOf(ctx, db.GetSubgroupOfParams{GroupID: groupID, ID: subID})
			if err != nil {
				return nil, orNotFound(err, "subgroup")
			}
			g, err := q.UpdateSubgroupOf(ctx, db.UpdateSubgroupOfParams{
				GroupID: groupID, ID: subID, Division: in.Division, Part: in.Part, Size: in.Size,
			})
			if err != nil {
				return nil, err
			}
			out := toSubgroup(g)
			c.Before, c.After = toSubgroup(before), out
			return out, nil
		}})
}

func (s *server) deleteSubgroup(w http.ResponseWriter, r *http.Request) {
	groupID, subID, err := subgroupPath(r, true)
	if err != nil {
		s.writeError(w, err, opDelete)
		return
	}
	s.mutate(w, r, mutation{entity: "subgroup", entityID: idString(subID), op: opDelete, status: http.StatusNoContent,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			before, err := q.GetSubgroupOf(ctx, db.GetSubgroupOfParams{GroupID: groupID, ID: subID})
			if err != nil {
				return nil, orNotFound(err, "subgroup")
			}
			c.Before = toSubgroup(before)
			_, err = q.DeleteSubgroupOf(ctx, db.DeleteSubgroupOfParams{GroupID: groupID, ID: subID})
			return nil, err
		}})
}

// ---------------------------------------------------------------- teachers

type teacher struct {
	ID        int64  `json:"id"`
	FullName  string `json:"full_name"`
	ShortName string `json:"short_name"`
}

type teacherInput struct {
	FullName  string `json:"full_name"`
	ShortName string `json:"short_name"`
}

func (in *teacherInput) validate() error {
	var v validator
	v.text("full_name", &in.FullName, 1, maxNameLen)
	v.text("short_name", &in.ShortName, 1, maxNameLen)
	return v.err()
}

func toTeacher(t db.Teacher) teacher {
	return teacher{ID: t.ID, FullName: t.FullName, ShortName: t.ShortName}
}

func (s *server) listTeachers(w http.ResponseWriter, r *http.Request) {
	s.read(w, r, func(ctx context.Context, q *db.Queries) (any, error) {
		rows, err := q.ListTeachers(ctx)
		return mapSlice(rows, toTeacher), err
	})
}

func (s *server) getTeacher(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.writeError(w, err, opRead)
		return
	}
	s.read(w, r, func(ctx context.Context, q *db.Queries) (any, error) {
		t, err := q.GetTeacher(ctx, id)
		return toTeacher(t), orNotFound(err, "teacher")
	})
}

func (s *server) createTeacher(w http.ResponseWriter, r *http.Request) {
	var in teacherInput
	if !s.decode(w, r, &in) {
		return
	}
	s.mutate(w, r, mutation{entity: "teacher", op: opWrite, status: http.StatusCreated,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			t, err := q.CreateTeacher(ctx, db.CreateTeacherParams{FullName: in.FullName, ShortName: in.ShortName})
			if err != nil {
				return nil, err
			}
			out := toTeacher(t)
			c.EntityID, c.After = idString(t.ID), out
			return out, nil
		}})
}

func (s *server) updateTeacher(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.writeError(w, err, opWrite)
		return
	}
	var in teacherInput
	if !s.decode(w, r, &in) {
		return
	}
	s.mutate(w, r, mutation{entity: "teacher", entityID: idString(id), op: opWrite, status: http.StatusOK,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			before, err := q.GetTeacher(ctx, id)
			if err != nil {
				return nil, orNotFound(err, "teacher")
			}
			t, err := q.UpdateTeacher(ctx, db.UpdateTeacherParams{ID: id, FullName: in.FullName, ShortName: in.ShortName})
			if err != nil {
				return nil, err
			}
			out := toTeacher(t)
			c.Before, c.After = toTeacher(before), out
			return out, nil
		}})
}

func (s *server) deleteTeacher(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.writeError(w, err, opDelete)
		return
	}
	s.mutate(w, r, mutation{entity: "teacher", entityID: idString(id), op: opDelete, status: http.StatusNoContent,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			before, err := q.GetTeacher(ctx, id)
			if err != nil {
				return nil, orNotFound(err, "teacher")
			}
			c.Before = toTeacher(before)
			_, err = q.DeleteTeacher(ctx, id)
			return nil, err
		}})
}

// ---------------------------------------------------------------- disciplines

type discipline struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type disciplineInput struct {
	Name string `json:"name"`
}

func (in *disciplineInput) validate() error {
	var v validator
	v.text("name", &in.Name, 1, maxNameLen)
	return v.err()
}

func toDiscipline(d db.Discipline) discipline { return discipline{ID: d.ID, Name: d.Name} }

func (s *server) listDisciplines(w http.ResponseWriter, r *http.Request) {
	s.read(w, r, func(ctx context.Context, q *db.Queries) (any, error) {
		rows, err := q.ListDisciplines(ctx)
		return mapSlice(rows, toDiscipline), err
	})
}

func (s *server) getDiscipline(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.writeError(w, err, opRead)
		return
	}
	s.read(w, r, func(ctx context.Context, q *db.Queries) (any, error) {
		d, err := q.GetDiscipline(ctx, id)
		return toDiscipline(d), orNotFound(err, "discipline")
	})
}

func (s *server) createDiscipline(w http.ResponseWriter, r *http.Request) {
	var in disciplineInput
	if !s.decode(w, r, &in) {
		return
	}
	s.mutate(w, r, mutation{entity: "discipline", op: opWrite, status: http.StatusCreated,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			d, err := q.CreateDiscipline(ctx, in.Name)
			if err != nil {
				return nil, err
			}
			out := toDiscipline(d)
			c.EntityID, c.After = idString(d.ID), out
			return out, nil
		}})
}

func (s *server) updateDiscipline(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.writeError(w, err, opWrite)
		return
	}
	var in disciplineInput
	if !s.decode(w, r, &in) {
		return
	}
	s.mutate(w, r, mutation{entity: "discipline", entityID: idString(id), op: opWrite, status: http.StatusOK,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			before, err := q.GetDiscipline(ctx, id)
			if err != nil {
				return nil, orNotFound(err, "discipline")
			}
			d, err := q.UpdateDiscipline(ctx, db.UpdateDisciplineParams{ID: id, Name: in.Name})
			if err != nil {
				return nil, err
			}
			out := toDiscipline(d)
			c.Before, c.After = toDiscipline(before), out
			return out, nil
		}})
}

func (s *server) deleteDiscipline(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.writeError(w, err, opDelete)
		return
	}
	s.mutate(w, r, mutation{entity: "discipline", entityID: idString(id), op: opDelete, status: http.StatusNoContent,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			before, err := q.GetDiscipline(ctx, id)
			if err != nil {
				return nil, orNotFound(err, "discipline")
			}
			c.Before = toDiscipline(before)
			_, err = q.DeleteDiscipline(ctx, id)
			return nil, err
		}})
}

// ---------------------------------------------------------------- shared

func mapSlice[T, U any](in []T, f func(T) U) []U {
	out := make([]U, len(in))
	for i, v := range in {
		out[i] = f(v)
	}
	return out
}
