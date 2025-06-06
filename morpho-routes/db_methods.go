package morphoroutes

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
	"io"
	"math/rand"
	"mime/multipart"
	"os"
	"time"
)

/*
	Project Methods
*/

func GetProject(db *sql.DB, projectName string) (Project, error) {
	if row, err := db.Query(
		"SELECT creation_date, project_name, variable_metadata, output_metadata, assets, deleted FROM project WHERE project_name=?",
		projectName,
	); err == nil {
		var p Project
		row.Next()
		row.Scan(&p.CreationDate, &p.ProjectName, &p.VariableMetadata, &p.OutputMetadata, &p.Assets, &p.Deleted)
		return p, err
	} else {
		return Project{}, err
	}
}

/*
Creates a project.

db is the database object.

Returns an error if either the object validation or the database write fails.
*/
func (p *Project) Create(tx *sql.Tx) error {
	if err := Validate(*p); err != nil {
		return err
	}

	vm, err := json.Marshal(p.VariableMetadata)
	if err != nil {
		return err
	}

	om, err := json.Marshal(p.OutputMetadata)
	if err != nil {
		return err
	}

	as, err := json.Marshal(p.Assets)
	if err != nil {
		return err
	}

	if _, err = tx.Exec(
		"INSERT INTO project (creation_date, project_name, variable_metadata, output_metadata, assets, deleted) VALUES (?, ?, ?, ?, ?, ?)",
		p.CreationDate,
		p.ProjectName,
		string(vm),
		string(om),
		string(as),
		0,
	); err != nil {
		return err
	}

	return nil
}

/*
	[]Solution Methods
*/

/*
Saves a list of Solution objects under a projectName.

db is the database object.
projectName is the name of the project to associate the solutions with.

An error is returned if the object validation or the database write fails.
*/
func (s SolutionSet) Create(tx *sql.Tx, projectName string) error {
	if err := Validate(s); err != nil {
		return err
	}

	stmt, err := tx.Prepare(
		"INSERT INTO solution (id, parameters, output_parameters, project_name, scoped_id) VALUES (?, ?, ?, ?, ?)",
	)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, solution := range s {
		if err := Validate(s); err != nil {
			return err
		}

		iparam, err := json.Marshal(solution.Parameter)
		if err != nil {
			return err
		}

		oparam, err := json.Marshal(solution.OutputParameter)
		if err != nil {
			return err
		}

		_, err = stmt.Exec(
			solution.Id,
			string(iparam),
			string(oparam),
			projectName,
			solution.ScopedId,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

/*
	Metadata Methods
*/

/*
Gets a metadata record from the database.

Returns an error if the database is unreachable or if the record does not exist.
*/
func GetMetadata(db *sql.DB, projectName string) (m Metadata, err error) {
	row := db.QueryRow("SELECT captions, human_name, slug, markdown FROM metadata WHERE project_name=?", projectName)
	err = row.Scan(&m.Captions, &m.HumanName, &m.Description.Slug, &m.Description.Text)
	return
}

func (m Metadata) Create(tx *sql.Tx, projectName string) error {
	if err := Validate(m); err != nil {
		return err
	}

	captions, err := json.Marshal(m.Captions)
	if err != nil {
		return err
	}

	_, err = tx.Exec(
		"INSERT INTO metadata (project_name, captions, human_name, slug, markdown) VALUES (?, ?, ?, ?, ?)",
		projectName,
		string(captions),
		m.HumanName,
		m.Description.Slug,
		m.Description.Text,
	)
	if err != nil {
		return err
	}

	return nil
}

/*
	Asset Methods
*/

/*
Creates and returns an s3 Client.

Returns an error if any step of the process fails.
*/
func CreateS3Client() (*s3.Client, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, err
	}

	client := s3.NewFromConfig(cfg)
	return client, nil
}

/*
Uploads assets to S3.

fileHeader is a file fetched from a multipart post form.
name is the name of the file on disk.

This method does not use any AWS credentials, and relies on the instance having IAM policies that allow GET/PUT/DELETE on the morpho-images bucket.

Returns an error if any step of the process fails, and the file's extension.
*/
func uploadAssetS3(fileHeader *multipart.FileHeader, name string) (string, error) {
	file, err := fileHeader.Open()
	if err != nil {
		return "", err
	}

	mime, err := mimetype.DetectReader(file)
	if err != nil {
		return "", err
	}

	file.Seek(0, io.SeekStart) // Detect reader consumes the beginning of the file. So we need to reset the head of the file back to the start.

	client, err := CreateS3Client()
	if err != nil {
		return "", nil
	}

	_, err = client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:      aws.String("morpho-images"),
		Key:         aws.String(fmt.Sprintf("assets/%s%s", name, mime.Extension())),
		Body:        file,
		ContentType: aws.String(mime.String()),
	})

	return mime.Extension(), err
}

/*
Uploads assets to a local folder. To be used for testing.

fileHeader is a file fetched from the multipart post form.
name is the name of the file on disk.

The problem with this method and the one above, is that there is no
mimetype checking really. While we could restrict this, it would make the tool
very unusable without a list of acceptable mimetypes.

Considering I'll be gone in a few months, it sounds like a bad idea to make users
specify MIME types to just upload a project. While I could allow some leeway
(allowing any image/x file on image/png, for example), that is time consuming
and I'm on a time crunch.

Hence,
TODO: Restrict MIME types sensibly.

Returns an error if any step of the process fails, and the file's extension.
*/
func uploadAssetsLocal(fileHeader *multipart.FileHeader, name string) (string, error) {
	file, err := fileHeader.Open()
	if err != nil {
		return "", err
	}

	mime, err := mimetype.DetectReader(file)
	if err != nil {
		return "", err
	}
	file.Close()

	file.Seek(0, io.SeekStart) // Detect reader consumes the beginning of the file. So we need to reset the head of the file back to the start.

	if writeHandle, err := os.OpenFile(fmt.Sprintf("assets/%s%s", name, mime.Extension()), os.O_CREATE|os.O_RDWR, 0644); err == nil {
		buffer, err := io.ReadAll(file)
		if err != nil {
			return "", err
		}

		n, err := writeHandle.Write(buffer)
		if n != len(buffer) || err != nil {
			return "", fmt.Errorf("could not write complete file: " + err.Error())
		}
		return mime.Extension(), nil
	} else {
		return "", err
	}
}

/*
Generates a random string with alphabetical characters.

length is the length of the string generated.
*/
func randString(length int) string {
	rand.New(rand.NewSource(time.Now().Unix()))
	randPart := []byte{}
	for range length {
		if rand.Intn(2) == 0 {
			randPart = append(randPart, byte(rand.Int31()%26+97))
		} else {
			randPart = append(randPart, byte(rand.Int31()%26+65))

		}
	}
	return string(randPart)
}

/*
Uploads the provided assets to S3 or the local filesystem and adds records into the database.

db is a database object.

filetags is a ProjectAssetField list which specifies the tags allowed.

solutionId specifies which solution the assets are going to be associated with

files is a cleaned multipart form. (i.e. each map key contains only uploaded file)

Returns an error if any step of the process fails.
*/
func CreateAssets(db *sql.DB, filetags []ProjectAssetField, solutionId string, scopedId int, files map[string]*multipart.FileHeader) error {
	// Uploads a file associated with an asset to the storage bucket.

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	tagMap := make(map[string]bool)
	for _, fileAsset := range filetags {
		tagMap[fileAsset.Tag] = true
	}

	// check if all the provided tags in the form exist. Terminate otherwise.
	for tag := range files {
		if _, ok := tagMap[tag]; !ok {
			return fmt.Errorf("tag %s does not exist on the solution", tag)
		}
	}

	for tag, handle := range files {
		auuid, err := uuid.NewRandom()
		if err != nil {
			return err
		}

		randomName := fmt.Sprintf("%d_%s", scopedId, randString(7))

		config, err := GetConfig()
		if err != nil {
			return err
		}

		var extension string
		if config.ENVIRONMENT == "prod" {
			extension, err = uploadAssetS3(handle, randomName)
			if err != nil {
				return err
			}
		} else {
			extension, err = uploadAssetsLocal(handle, randomName)
			if err != nil {
				return err
			}
		}

		if _, err = tx.Exec(
			"INSERT INTO asset (id, file, tag, solution_id) VALUES (?, ?, ?, ?)",
			auuid.String(),
			fmt.Sprintf("assets/%s%s", randomName, extension),
			tag,
			solutionId,
		); err != nil {
			return err
		}
	}

	if err = tx.Commit(); err != nil {
		return err
	}

	return nil
}
