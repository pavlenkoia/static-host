package publication

import (
 "context"
 "crypto/rand"
 "database/sql"
 "encoding/hex"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "syscall"
 "sync"
 "time"
 "unicode/utf8"
)

type Identity struct { OwnerID int64; KeyID string }
type Options struct { UploadOrigin, SiteSuffix string; Local bool }
type UploadStatus struct { ID string `json:"id"`; State string `json:"state"`; ExpiresAt int64 `json:"expires_at"`; ErrorCode *string `json:"error_code"` }
type Site struct { ID string `json:"id"`; Name string `json:"name"`; URL string `json:"url"`; Status string `json:"status"`; Generation int64 `json:"generation"`; SizeBytes int64 `json:"size_bytes"`; FileCount int64 `json:"file_count"`; CreatedAt int64 `json:"created_at"`; UpdatedAt int64 `json:"updated_at"`; Upload *UploadStatus `json:"upload"` }
type Error struct { Code string }
func (e *Error) Error() string { return e.Code }
func failure(code string) error { return &Error{Code:code} }

type Service struct { db *sql.DB; dataDir string; options Options; mu sync.Mutex; locksMu sync.Mutex; locks map[string]*sync.RWMutex; stop chan struct{}; done chan struct{}; failpoint func(string); spaceCheck func(int64) bool; now func() time.Time }

func New(db *sql.DB, dataDir string, options Options) (*Service,error) {
 s:=&Service{db:db,dataDir:dataDir,options:options,locks:make(map[string]*sync.RWMutex),stop:make(chan struct{}),done:make(chan struct{}),now:time.Now}
 for _,dir:=range []string{"staging","content"} { if err:=os.MkdirAll(filepath.Join(dataDir,"sites",dir),0700); err!=nil { return nil,err } }
 var stagingStat,contentStat syscall.Stat_t
 if err:=syscall.Stat(filepath.Join(dataDir,"sites","staging"),&stagingStat); err!=nil {return nil,err}
 if err:=syscall.Stat(filepath.Join(dataDir,"sites","content"),&contentStat); err!=nil {return nil,err}
 if stagingStat.Dev!=contentStat.Dev {return nil,errors.New("publication staging and content must share a filesystem")}
 if err:=s.reconcile(true); err!=nil {return nil,err}
 go s.janitor()
 return s,nil
}
func (s *Service) Close() error { close(s.stop); <-s.done; return nil }
func randomID() (string,error) { var b [16]byte; if _,err:=rand.Read(b[:]); err!=nil {return "",err}; return hex.EncodeToString(b[:]),nil }
func (s *Service) siteURL(id string) string { scheme:="https://"; if s.options.Local {scheme="http://"}; return scheme+id+"."+s.options.SiteSuffix }
func (s *Service) Create(ctx context.Context, ownerID int64, name string) (Site,error) {
 name=strings.TrimSpace(name); if !utf8.ValidString(name) || utf8.RuneCountInString(name)<1 || utf8.RuneCountInString(name)>80 {return Site{},failure("invalid_request")}
 s.mu.Lock(); defer s.mu.Unlock()
 var count int; if err:=s.db.QueryRowContext(ctx,"SELECT count(*) FROM sites WHERE owner_id=? AND deleted_at IS NULL",ownerID).Scan(&count); err!=nil {return Site{},err}; if count>=100 {return Site{},failure("site_limit")}
 id,err:=randomID(); if err!=nil {return Site{},err}; id="s-"+id; now:=s.now().Unix()
 if _,err=s.db.ExecContext(ctx,"INSERT INTO sites(id,owner_id,name,created_at,updated_at) VALUES(?,?,?,?,?)",id,ownerID,name,now,now); err!=nil {return Site{},err}
 return Site{ID:id,Name:name,URL:s.siteURL(id),Status:"draft",CreatedAt:now,UpdatedAt:now},nil
}
func (s *Service) List(ctx context.Context,ownerID int64) ([]Site,error) {
 rows,err:=s.db.QueryContext(ctx,"SELECT id FROM sites WHERE owner_id=? AND deleted_at IS NULL ORDER BY created_at,id",ownerID); if err!=nil {return nil,err}; ids:=[]string{}; for rows.Next(){var id string; if err=rows.Scan(&id); err!=nil {break}; ids=append(ids,id)}; if err==nil {err=rows.Err()}; rows.Close(); if err!=nil {return nil,err}
 sites:=make([]Site,0,len(ids)); for _,id:=range ids {site,e:=s.Get(ctx,ownerID,id); if e!=nil {return nil,e}; sites=append(sites,site)}; return sites,nil
}
func (s *Service) Get(ctx context.Context,ownerID int64,id string) (Site,error) {
 var site Site; var dir sql.NullString
 err:=s.db.QueryRowContext(ctx,"SELECT id,name,generation,current_dir,size_bytes,file_count,created_at,updated_at FROM sites WHERE id=? AND owner_id=? AND deleted_at IS NULL",id,ownerID).Scan(&site.ID,&site.Name,&site.Generation,&dir,&site.SizeBytes,&site.FileCount,&site.CreatedAt,&site.UpdatedAt)
 if errors.Is(err,sql.ErrNoRows) {return Site{},failure("site_not_found")}; if err!=nil {return Site{},err}; site.URL=s.siteURL(site.ID); site.Status="draft"; if dir.Valid {site.Status="published"}
 var u UploadStatus; err=s.db.QueryRowContext(ctx,"SELECT id,state,expires_at,error_code FROM uploads WHERE site_id=? ORDER BY rowid DESC LIMIT 1",id).Scan(&u.ID,&u.State,&u.ExpiresAt,&u.ErrorCode); if err==nil && u.State!="published" {site.Upload=&u} else if err!=nil && !errors.Is(err,sql.ErrNoRows) {return Site{},err}; return site,nil
}
