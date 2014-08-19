package resource

import (
	"database/sql"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/jinzhu/gorm"
	"github.com/qor/qor"
	"github.com/qor/qor/media_library"
	"github.com/qor/qor/rules"
)

type Meta struct {
	Base          Resourcer
	Name          string
	Alias         string
	Type          string
	Label         string
	Value         func(interface{}, *qor.Context) interface{}
	Setter        func(resource interface{}, metaValues *MetaValues, context *qor.Context)
	Collection    interface{}
	GetCollection func(interface{}, *qor.Context) [][]string
	Resource      Resourcer
	Permission    *rules.Permission
}

type Metaor interface {
	GetMeta() *Meta
	HasPermission(rules.PermissionMode, *qor.Context) bool
}

func (meta *Meta) GetMeta() *Meta {
	return meta
}

func (meta *Meta) HasPermission(mode rules.PermissionMode, context *qor.Context) bool {
	if meta.Permission == nil {
		return true
	}
	return meta.Permission.HasPermission(mode, context)
}

func (meta *Meta) UpdateMeta() {
	var hasColumn bool
	var valueType string

	if meta.Name == "" {
		qor.ExitWithMsg("Meta should have name: %v", reflect.ValueOf(meta).Type())
	} else {
		if meta.Label == "" {
			meta.Label = strings.Title(meta.Name)
		}
		if meta.Alias == "" {
			meta.Alias = meta.Name
		}
		meta.Alias = gorm.SnakeToUpperCamel(meta.Alias)
	}

	base := meta.Base.GetResource()
	scope := &gorm.Scope{Value: base.Value}
	var field *gorm.Field
	field, hasColumn = scope.FieldByName(meta.Alias)
	if hasColumn {
		valueType = reflect.TypeOf(field.Value).Kind().String()
	}

	// Set Meta Type
	if meta.Type == "" {
		if relationship := field.Relationship; relationship != nil {
			if relationship.Kind == "belongs_to" || relationship.Kind == "has_one" {
				meta.Type = "single_edit"
			} else if relationship.Kind == "has_many" {
				meta.Type = "collection_edit"
			} else if relationship.Kind == "many_to_many" {
				meta.Type = "select_many"
			}
		} else {
			switch valueType {
			case "string":
				meta.Type = "string"
			case "bool":
				meta.Type = "checkbox"
			default:
				if regexp.MustCompile(`^(u)?(int|float)(\d+)?`).MatchString(valueType) {
					meta.Type = "number"
				} else if _, ok := field.Value.(media_library.MediaLibrary); ok {
					meta.Type = "file"
				}
			}
		}
	}

	// Set Meta Resource
	if meta.Resource == nil {
		if hasColumn && (field.Relationship != nil) {
			var result interface{}
			if valueType == "struct" {
				result = reflect.New(reflect.Indirect(reflect.ValueOf(field.Value)).Type()).Interface()
			} else if valueType == "slice" {
				result = reflect.New(reflect.Indirect(reflect.ValueOf(field.Value)).Type().Elem()).Interface()
			}

			resource := reflect.New(reflect.Indirect(reflect.ValueOf(meta.Base)).Type()).Interface()
			if resourcer, ok := resource.(Resourcer); ok {
				res := resourcer.GetResource()
				res.Value = result
				meta.Resource = resourcer
			}
		}
	}

	// Set Meta Value
	if meta.Value == nil {
		if hasColumn {
			meta.Value = func(value interface{}, context *qor.Context) interface{} {
				scope := &gorm.Scope{Value: value}
				if f, ok := scope.FieldByName(meta.Alias); ok {
					if field.Relationship != nil {
						if !reflect.ValueOf(f.Value).CanAddr() {
							if reflect.ValueOf(f.Value).Kind() == reflect.Slice {
								sliceType := reflect.ValueOf(f.Value).Type()
								slice := reflect.MakeSlice(sliceType, 0, 0)
								slicePtr := reflect.New(sliceType)
								slicePtr.Elem().Set(slice)
								f.Value = slicePtr.Interface()
							} else if reflect.ValueOf(f.Value).Kind() == reflect.Struct {
								f.Value = reflect.New(reflect.Indirect(reflect.ValueOf(f.Value)).Type()).Interface()
							}
						}

						context.DB().Model(value).Related(f.Value, meta.Alias)
					}
					return f.Value
				}
				return ""
			}
		} else {
			qor.ExitWithMsg("Unsupported meta name %v for resource %v", meta.Name, reflect.TypeOf(base.Value))
		}
	}

	// Set Meta Collection
	if meta.Collection != nil {
		if maps, ok := meta.Collection.([]string); ok {
			meta.GetCollection = func(interface{}, *qor.Context) (results [][]string) {
				for _, value := range maps {
					results = append(results, []string{value, value})
				}
				return
			}
		} else if maps, ok := meta.Collection.([][]string); ok {
			meta.GetCollection = func(interface{}, *qor.Context) [][]string {
				return maps
			}
		} else if f, ok := meta.Collection.(func(interface{}, *qor.Context) [][]string); ok {
			meta.GetCollection = f
		} else {
			qor.ExitWithMsg("Unsupported Collection format for meta %v of resource %v", meta.Name, reflect.TypeOf(base.Value))
		}
	} else if meta.Type == "select_one" || meta.Type == "select_many" {
		qor.ExitWithMsg("%v meta type %v needs Collection", meta.Name, meta.Type)
	}

	if meta.Setter == nil {
		meta.Setter = func(resource interface{}, metaValues *MetaValues, context *qor.Context) {
			metaValue := metaValues.Get(meta.Name)
			if metaValue == nil {
				return
			}
			value := metaValue.Value
			scope := &gorm.Scope{Value: resource}
			scopeField, _ := scope.FieldByName(meta.Alias)
			field := reflect.Indirect(reflect.ValueOf(resource)).FieldByName(meta.Alias)

			if field.IsValid() && field.CanAddr() {
				relationship := scopeField.Relationship
				if relationship != nil && relationship.Kind == "many_to_many" {
					context.DB().Where(ToArray(value)).Find(field.Addr().Interface())
					if !scope.PrimaryKeyZero() {
						context.DB().Model(resource).Association(meta.Alias).Replace(field.Interface())
					}
				} else {
					switch field.Kind() {
					case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
						field.SetInt(ToInt(value))
					case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
						field.SetUint(ToUint(value))
					case reflect.Float32, reflect.Float64:
						field.SetFloat(ToFloat(value))
					default:
						if scanner, ok := field.Addr().Interface().(sql.Scanner); ok {
							scanner.Scan(ToString(value))
						} else if reflect.TypeOf(ToArray(value)).ConvertibleTo(field.Type()) {
							field.Set(reflect.ValueOf(ToArray(value)).Convert(field.Type()))
						} else if reflect.TypeOf(ToString(value)).ConvertibleTo(field.Type()) {
							field.Set(reflect.ValueOf(ToString(value)).Convert(field.Type()))
						} else {
							qor.ExitWithMsg("Can't set value", meta, meta.Base)
						}
					}
				}
				// if headers, ok := context.Request.MultipartForm.File[value.(string)]; ok {
				// 	for _, header := range headers {
				// 		if media, ok := field.Interface().(media_library.MediaLibrary); ok {
				// 			if file, err := header.Open(); err == nil {
				// 				media.SetFile(header.Filename, file)
				// 			}
				// 		}
				// 	}
				// }
			}
		}
	}
}

func ToArray(value interface{}) (values []string) {
	if v, ok := value.([]string); ok {
		return v
	} else if v, ok := value.(string); ok {
		return []string{v}
	} else if vs, ok := value.([]interface{}); ok {
		for _, v := range vs {
			values = append(values, fmt.Sprintf("%v", v))
		}
	}
	return
}

func ToString(value interface{}) string {
	if v, ok := value.([]string); ok && len(v) > 0 {
		return v[0]
	} else if v, ok := value.(string); ok {
		return v
	}
	return ""
}

func ToInt(value interface{}) int64 {
	var result string
	if v, ok := value.([]string); ok && len(v) > 0 {
		result = v[0]
	} else if v, ok := value.(string); ok {
		result = v
	}
	// TODO: hiding error here could be a problem?
	// i, _ := strconv.Atoi(result)
	n, _ := strconv.ParseInt(result, 10, 64)
	return n
}

func ToUint(value interface{}) uint64 {
	var result string
	if v, ok := value.([]string); ok && len(v) > 0 {
		result = v[0]
	} else if v, ok := value.(string); ok {
		result = v
	}
	// TODO: hiding error here could be a problem?
	n, _ := strconv.ParseUint(result, 10, 64)
	return n
}

func ToFloat(value interface{}) float64 {
	var result string
	if v, ok := value.([]string); ok && len(v) > 0 {
		result = v[0]
	} else if v, ok := value.(string); ok {
		result = v
	}
	// TODO: hiding error here could be a problem?
	n, _ := strconv.ParseFloat(result, 64)
	return n
}
